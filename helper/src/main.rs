use anyhow::{bail, Context, Result};
use rattler::install::{Installer, LinkOptions, ExternalSymlinkPolicy};
use rattler_cache::package_cache::PackageCache;
use rattler_conda_types::{Channel, ChannelConfig, MatchSpec, PrefixRecord, RepoData, Subdir};
use rattler_solve::{resolvo::Solver, ChannelPriority, SolverImpl, SolverTask};
use serde::Deserialize;
use std::{io::Read, path::PathBuf, str::FromStr};
use url::Url;

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
}

fn validate_policy(request: &Request) -> Result<()> {
    if request.network.as_ref().is_some_and(|policy| policy.allow_https) {
        bail!("HTTPS gateway is not implemented; network policy cannot be honored");
    }
    Ok(())
}

#[tokio::main]
async fn main() {
    if let Err(error) = run().await {
        eprintln!("{error:#}");
        std::process::exit(1);
    }
}

async fn run() -> Result<()> {
    let mut input = String::new();
    std::io::stdin().take(1024 * 1024 + 1).read_to_string(&mut input)?;
    if input.len() > 1024 * 1024 { bail!("request too large"); }
    let request: Request = serde_json::from_str(&input).context("invalid helper request")?;
    validate_policy(&request)?;
    if request.protocol != 1 || request.channels.is_empty() || request.packages.is_empty() {
        bail!("protocol must be 1 and channels and packages must be nonempty");
    }
    let platform = Subdir::from_str(&request.platform)?;
    if Some(platform) != Subdir::current() { bail!("native platform required"); }
    let specs = request.packages.iter().map(|s| s.parse::<MatchSpec>()).collect::<Result<Vec<_>, _>>()?;
    let config = ChannelConfig::default_with_root_dir(std::env::current_dir()?);
    let mut repositories = Vec::new();
    for source in &request.channels {
        let path = if source.starts_with("file:") {
            Url::parse(source)?.to_file_path().map_err(|_| anyhow::anyhow!("invalid file channel"))?
        } else {
            if source.contains("://") { bail!("only local channels are supported"); }
            PathBuf::from(source)
        }.canonicalize().context("local channel does not exist")?;
        let channel_url = Url::from_directory_path(&path).map_err(|_| anyhow::anyhow!("invalid channel path"))?;
        let channel = Channel::from_str(channel_url.as_str(), &config)?;
        for subdir in [platform.to_string(), "noarch".to_string()] {
            let metadata = path.join(&subdir).join("repodata.json");
            if !metadata.exists() { continue; }
            let repo = RepoData::from_path(&metadata).context("reading local repodata")?;
            let records = repo.into_repo_data_records(&channel);
            for record in &records {
                if record.package_record.sha256.is_none() { bail!("package archive requires SHA-256"); }
                let file = record.url.to_file_path().map_err(|_| anyhow::anyhow!("repodata references a non-local package"))?;
                let file = file.canonicalize().context("package archive does not exist")?;
                if !file.starts_with(&path) { bail!("package archive escapes its channel"); }
            }
            repositories.push(records);
        }
    }
    let installed = PrefixRecord::collect_from_prefix::<PrefixRecord>(&request.prefix)?;
    let result = Solver.solve(SolverTask {
        virtual_packages: rattler_virtual_packages::VirtualPackages::detect(&Default::default(), None)?.into_generic_virtual_packages().collect(),
        specs: specs.clone(),
        channel_priority: ChannelPriority::Strict,
        locked_packages: installed.iter().map(|r| &r.repodata_record).collect(),
        ..SolverTask::from_iter(&repositories)
    })?;
    if !installed.is_empty() { bail!("Rattler requires an unowned Conda prefix"); }
    Installer::new()
        .with_execute_link_scripts(false)
        .with_external_symlink_policy(ExternalSymlinkPolicy::Deny)
        .with_link_options(LinkOptions { allow_hard_links: Some(false), allow_symbolic_links: None, allow_ref_links: Some(false) })
        .with_package_cache(PackageCache::new(request.cache))
        .with_target_platform(platform)
        .with_installed_packages(installed)
        .with_requested_specs(specs)
        .install(&request.prefix, result.records).await?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn network_policy_fails_closed() {
        let base = r#"{"protocol":1,"prefix":"p","cache":"c","platform":"linux-64","channels":["local"],"packages":["x"]"#;
        for (suffix, accepted) in [("}", true), (r#", "network":{"allow_https":false}}"#, true), (r#", "network":{"allow_https":true}}"#, false)] {
            let request: Request = serde_json::from_str(&format!("{base}{suffix}")).unwrap();
            assert_eq!(validate_policy(&request).is_ok(), accepted);
        }
        assert!(serde_json::from_str::<Request>(&format!("{base}{}", r#", "network":{"allow_https":false,"proxy":"x"}}"#)).is_err());
    }
}
