use anyhow::{Context, Result, bail};
use rattler::install::{ExternalSymlinkPolicy, Installer, LinkOptions};
use rattler_cache::package_cache::PackageCache;
use rattler_conda_types::{Channel, ChannelConfig, MatchSpec, PrefixRecord, RepoData, Subdir};
use rattler_repodata_gateway::{ChannelConfig as GatewayChannelConfig, Gateway, SourceConfig};
use rattler_solve::{ChannelPriority, SolverImpl, SolverTask, resolvo::Solver};
use reqwest_middleware::ClientBuilder;
use serde::Deserialize;
use std::{io::Read, path::PathBuf, str::FromStr};
use url::Url;

// Enforce channel boundaries on metadata and shard URLs as well as package archives.
struct ChannelScope {
    channels: Vec<Url>,
    authorization: Option<reqwest::header::HeaderValue>,
}
#[async_trait::async_trait]
impl reqwest_middleware::Middleware for ChannelScope {
    async fn handle(
        &self,
        mut req: reqwest::Request,
        extensions: &mut http::Extensions,
        next: reqwest_middleware::Next<'_>,
    ) -> reqwest_middleware::Result<reqwest::Response> {
        let url = req.url();
        if !url.username().is_empty()
            || url.password().is_some()
            || !self
                .channels
                .iter()
                .any(|base| url.origin() == base.origin() && url.path().starts_with(base.path()))
        {
            return Err(reqwest_middleware::Error::middleware(
                std::io::Error::other("outside configured channel"),
            ));
        }
        if let Some(value) = &self.authorization {
            req.headers_mut()
                .insert(reqwest::header::AUTHORIZATION, value.clone());
        }
        next.run(req, extensions).await
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    protocol: u32,
    prefix: PathBuf,
    cache: PathBuf,
    platform: String,
    channels: Vec<String>,
    packages: Vec<String>,
    #[serde(default)]
    network: Option<NetworkPolicy>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NetworkPolicy {
    allow_https: bool,
    ca_bundle_pem: Option<String>,
    proxy_url: Option<String>,
    authorization: Option<String>,
}

#[tokio::main]
async fn main() {
    if let Err(error) = run().await {
        eprintln!("{error:#}");
        std::process::exit(1);
    }
}

async fn run() -> Result<()> {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.len() == 1 && args[0] == "--version" {
        println!("{}", env!("CARGO_PKG_VERSION"));
        return Ok(());
    }
    if args.len() == 1 && args[0] == "--protocol-version" {
        println!("1");
        return Ok(());
    }
    if args.len() == 2 && args[0] == "activate" {
        use rattler_shell::{
            activation::{ActivationVariables, Activator},
            shell::{Bash, CmdExe, ShellEnum},
        };
        let shell = if cfg!(windows) {
            ShellEnum::CmdExe(CmdExe)
        } else {
            ShellEnum::Bash(Bash::default())
        };
        let activation = Activator::from_path(
            std::path::Path::new(&args[1]),
            shell,
            Subdir::current().context("unsupported platform")?,
        )?;
        let before = rattler_shell::environment::EnvironmentSnapshot::from_system();
        let limits = rattler_shell::process::ProcessLimits {
            timeout: std::time::Duration::from_secs(120),
            output_limit: 8 * 1024 * 1024,
        };
        let after = activation
            .run_activation_bounded(ActivationVariables::from_env()?, &before, limits)
            .await?;
        let mut delta = std::collections::BTreeMap::new();
        for (key, value) in after.iter() {
            if before.get(key) != Some(value) {
                delta.insert(key.to_str().context("invalid activation key")?,
                             value.to_str().context("invalid activation value")?);
            }
        }
        for (key, _) in before.iter() {
            if after.get(key).is_none() {
                // RCC activation files represent removals with an empty value.
                delta.insert(key.to_str().context("invalid activation key")?, "");
            }
        }
        println!("{}", serde_json::to_string(&delta)?);
        return Ok(());
    }
    if args.len() == 2 && args[0] == "list" {
        let records =
            PrefixRecord::collect_from_prefix::<PrefixRecord>(std::path::Path::new(&args[1]))?;
        let packages:Vec<_>=records.iter().map(|p|serde_json::json!({"name":p.repodata_record.package_record.name.as_normalized(),"version":p.repodata_record.package_record.version.to_string(),"channel":p.repodata_record.channel})).collect();
        println!("{}", serde_json::to_string(&packages)?);
        return Ok(());
    }
    if !args.is_empty() {
        bail!("unknown helper command")
    }
    let mut input = String::new();
    std::io::stdin()
        .take(1024 * 1024 + 1)
        .read_to_string(&mut input)?;
    if input.len() > 1024 * 1024 {
        bail!("request too large");
    }
    let request: Request = serde_json::from_str(&input).context("invalid helper request")?;
    if request.protocol != 1 || request.channels.is_empty() || request.packages.is_empty() {
        bail!("protocol must be 1 and channels and packages must be nonempty");
    }
    let platform = Subdir::from_str(&request.platform)?;
    if Some(platform) != Subdir::current() {
        bail!("native platform required");
    }
    let specs = request
        .packages
        .iter()
        .map(|s| s.parse::<MatchSpec>())
        .collect::<Result<Vec<_>, _>>()?;
    let config = ChannelConfig::default_with_root_dir(std::env::current_dir()?);
    let remote = request.channels.iter().any(|c| c.starts_with("https://"));
    if remote
        && (request.channels.iter().any(|c| !c.starts_with("https://"))
            || !request.network.as_ref().is_some_and(|p| p.allow_https))
    {
        bail!("HTTPS requires an enabled network policy and exclusively HTTPS channels");
    }
    let mut builder = reqwest::Client::builder()
        .https_only(true)
        .no_proxy()
        .redirect(reqwest::redirect::Policy::none());
    if let Some(pem) = request
        .network
        .as_ref()
        .and_then(|p| p.ca_bundle_pem.as_ref())
    {
        for cert in reqwest::Certificate::from_pem_bundle(pem.as_bytes())? {
            builder = builder.add_root_certificate(cert);
        }
    }
    let scope = request
        .channels
        .iter()
        .filter(|c| c.starts_with("https://"))
        .map(|c| Url::parse(&format!("{}/", c.trim_end_matches('/'))))
        .collect::<Result<Vec<_>, _>>()?;
    let mut authorization = None;
    if let Some(policy) = &request.network {
        if policy.proxy_url.is_some() || policy.authorization.is_some() {
            let first = scope
                .first()
                .context("network policy requires an HTTPS channel")?;
            if !policy.allow_https
                || scope
                    .iter()
                    .any(|channel| channel.origin() != first.origin())
            {
                bail!("configured network route requires a single HTTPS origin");
            }
        }
        if let Some(raw) = &policy.proxy_url {
            let url = Url::parse(raw).map_err(|_| anyhow::anyhow!("invalid proxy URL"))?;
            if !matches!(url.scheme(), "http" | "https")
                || url.host().is_none()
                || url.path() != "/"
                || url.query().is_some()
                || url.fragment().is_some()
            {
                bail!("unsupported proxy URL");
            }
            let proxy = reqwest::Proxy::https(url)
                .map_err(|_| anyhow::anyhow!("invalid HTTPS proxy configuration"))?;
            builder = builder.proxy(proxy);
        }
        if let Some(raw) = &policy.authorization {
            if !raw.starts_with("Basic ") {
                bail!("unsupported channel authentication");
            }
            let mut value = reqwest::header::HeaderValue::from_str(raw)
                .context("invalid authorization header")?;
            value.set_sensitive(true);
            authorization = Some(value);
        }
    }
    let client = ClientBuilder::new(builder.build()?)
        .with(ChannelScope {
            channels: scope,
            authorization,
        })
        .build();
    let mut repositories = Vec::new();
    let mut remote_channels = Vec::new();
    if remote {
        for source in &request.channels {
            let mut url = Url::parse(source)?;
            if url.scheme() != "https"
                || url.host_str().is_none()
                || !url.username().is_empty()
                || url.password().is_some()
                || url.query().is_some()
                || url.fragment().is_some()
            {
                bail!("invalid HTTPS channel");
            }
            if !url.path().ends_with('/') {
                url.set_path(&format!("{}/", url.path()));
            }
            remote_channels.push(Channel::from_str(url.as_str(), &config)?);
        }
        let gateway = Gateway::builder()
            .with_cache_dir(request.cache.join("repodata"))
            .with_client(client.clone())
            .with_max_concurrent_requests(8)
            .with_channel_config(GatewayChannelConfig {
                default: SourceConfig {
                    sharded_enabled: true,
                    ..Default::default()
                },
                per_channel: Default::default(),
            })
            .finish();
        let data = gateway
            .query(
                remote_channels.clone(),
                [platform, Subdir::NoArch],
                specs.clone(),
            )
            .recursive(true)
            .await?;
        repositories.extend(
            data.iter()
                .map(|repo| repo.iter().cloned().collect::<Vec<_>>()),
        );
    } else {
        for source in &request.channels {
            let path = if source.starts_with("file:") {
                Url::parse(source)?
                    .to_file_path()
                    .map_err(|_| anyhow::anyhow!("invalid file channel"))?
            } else {
                if source.contains("://") {
                    bail!("only local channels are supported");
                }
                PathBuf::from(source)
            }
            .canonicalize()
            .context("local channel does not exist")?;
            let channel_url = Url::from_directory_path(&path)
                .map_err(|_| anyhow::anyhow!("invalid channel path"))?;
            let channel = Channel::from_str(channel_url.as_str(), &config)?;
            for subdir in [platform.to_string(), "noarch".to_string()] {
                let metadata = path.join(&subdir).join("repodata.json");
                if !metadata.exists() {
                    continue;
                }
                let repo = RepoData::from_path(&metadata).context("reading local repodata")?;
                let records = repo.into_repo_data_records(&channel);
                for record in &records {
                    if record.package_record.sha256.is_none() {
                        bail!("package archive requires SHA-256");
                    }
                    let file = record
                        .url
                        .to_file_path()
                        .map_err(|_| anyhow::anyhow!("repodata references a non-local package"))?;
                    let file = file
                        .canonicalize()
                        .context("package archive does not exist")?;
                    if !file.starts_with(&path) {
                        bail!("package archive escapes its channel");
                    }
                }
                repositories.push(records);
            }
        }
    }
    let installed = PrefixRecord::collect_from_prefix::<PrefixRecord>(&request.prefix)?;
    let result = Solver.solve(SolverTask {
        virtual_packages: rattler_virtual_packages::VirtualPackages::detect(
            &Default::default(),
            None,
        )?
        .into_generic_virtual_packages()
        .collect(),
        specs: specs.clone(),
        channel_priority: ChannelPriority::Strict,
        locked_packages: installed.iter().map(|r| &r.repodata_record).collect(),
        ..SolverTask::from_iter(&repositories)
    })?;
    if remote {
        for record in &result.records {
            if record.package_record.sha256.is_none() {
                bail!("package archive requires SHA-256");
            }
            let url = &record.url;
            if url.scheme() != "https"
                || !url.username().is_empty()
                || url.password().is_some()
                || url.query().is_some()
                || url.fragment().is_some()
                || !remote_channels.iter().any(|channel| {
                    let base: &Url = channel.base_url.as_ref();
                    url.origin() == base.origin()
                        && url.path().starts_with(base.path())
                        && record.channel.as_deref().is_some_and(|c| {
                            c.trim_end_matches('/') == base.as_str().trim_end_matches('/')
                        })
                })
            {
                bail!("package archive does not belong to its HTTPS channel");
            }
        }
    }
    if !installed.is_empty() {
        bail!("Rattler requires an unowned Conda prefix");
    }
    Installer::new()
        .with_download_client(client)
        .with_concurrent_requests_semaphore(std::sync::Arc::new(tokio::sync::Semaphore::new(8)))
        .with_execute_link_scripts(false)
        .with_external_symlink_policy(ExternalSymlinkPolicy::Deny)
        .with_link_options(LinkOptions {
            allow_hard_links: Some(false),
            allow_symbolic_links: None,
            allow_ref_links: Some(false),
        })
        .with_package_cache(PackageCache::new(request.cache))
        .with_target_platform(platform)
        .with_installed_packages(installed)
        .with_requested_specs(specs)
        .install(&request.prefix, result.records)
        .await?;
    Ok(())
}
