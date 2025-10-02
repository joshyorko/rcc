# rccremote --expose Feature

## Overview

The `--expose` flag allows `rccremote` to create instant public HTTPS URLs using **Cloudflare Quick Tunnels**, enabling remote access to your local rccremote server without any configuration, tokens, or Cloudflare account.

This feature is perfect for:
- 🚀 Quick demos and testing
- 🔧 Remote debugging
- 👥 Sharing holotree catalogs with team members
- 🌍 Accessing local environments from anywhere

## Prerequisites

Install the `cloudflared` binary (one-time setup):

### Ubuntu/Debian
```bash
curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb -o cloudflared.deb
sudo dpkg -i cloudflared.deb
```

### macOS
```bash
brew install cloudflare/cloudflare/cloudflared
```

### Windows
```bash
winget install cloudflare.cloudflared
```

For other platforms, see: https://github.com/cloudflare/cloudflared/releases

## Usage

### Phase 1: Quick Tunnel (Zero Configuration)

Start rccremote with public exposure using a random URL:

```bash
rccremote --expose
```

Output:
```
Remote for rcc starting (v18.8.0) ...
Starting Cloudflare Quick Tunnel...

  🌍 Public URL: https://seventy-nine-empty-ladybugs.trycloudflare.com
  🔐 Tunnel Status: Connected (Quick Tunnel)

...
```

**Features:**
- ✅ **No tokens required** - works immediately
- ✅ **Random subdomain** - generated automatically each run
- ✅ **Instant SSL/TLS** - valid HTTPS certificate included
- ✅ **~2 second startup** - very fast
- ✅ **Zero configuration** - no Cloudflare account needed

### Phase 2: Named Tunnel (Custom Domain)

For a persistent custom domain, use a Named Tunnel:

```bash
export CF_TUNNEL_TOKEN="your-tunnel-token"
rccremote --expose --tunnel-name my-tunnel
```

Output:
```
Remote for rcc starting (v18.8.0) ...
Starting Cloudflare Named Tunnel: my-tunnel

  🌍 Public URL: https://rccremote.yourdomain.com
  🔐 Tunnel Status: Connected (Named Tunnel: my-tunnel)

...
```

**Features:**
- ✅ **Custom domain** - use your own subdomain
- ✅ **Persistent URL** - same URL across restarts
- ✅ **Enterprise ready** - full Cloudflare features
- ⚠️  **Requires setup** - needs Cloudflare account and tunnel configuration

## Configuration

### Quick Tunnel (Default)
- **Flag:** `--expose`
- **Requirements:** Just the `cloudflared` binary
- **Configuration:** None needed
- **URL:** Random (e.g., `https://random-words.trycloudflare.com`)

### Named Tunnel
- **Flag:** `--expose --tunnel-name <name>`
- **Requirements:** 
  - `cloudflared` binary
  - Cloudflare account
  - Configured tunnel
  - `CF_TUNNEL_TOKEN` environment variable
- **URL:** Custom (configured in Cloudflare)

## Examples

### Basic Usage
```bash
# Start with Quick Tunnel
rccremote --expose

# Start with custom port
rccremote --expose --port 5000

# Start with Named Tunnel
export CF_TUNNEL_TOKEN="eyJhIjoiNGU..."
rccremote --expose --tunnel-name prod-server
```

### Testing the Tunnel
```bash
# In another terminal, test the public URL
curl https://your-tunnel-url.trycloudflare.com/parts/

# Or access via browser
open https://your-tunnel-url.trycloudflare.com/parts/
```

## Troubleshooting

### Error: "cloudflared not found"
**Solution:** Install the `cloudflared` binary (see Prerequisites above)

### Error: "timeout waiting for public URL"
**Possible causes:**
- Network connectivity issues
- Firewall blocking cloudflared
- cloudflared process crashed

**Solution:** 
- Check network connection
- Run with `--debug` flag for detailed logs
- Verify cloudflared works: `cloudflared --version`

### Error: "CF_TUNNEL_TOKEN environment variable required"
**Cause:** Using `--tunnel-name` without setting the token

**Solution:** 
```bash
export CF_TUNNEL_TOKEN="your-token-here"
rccremote --expose --tunnel-name my-tunnel
```

## Security Considerations

### Quick Tunnels
- URLs are **random and unpredictable** (but public once known)
- URLs **change on each restart** (ephemeral)
- No authentication built-in - consider adding your own
- Suitable for temporary/development use

### Named Tunnels
- URLs are **persistent** (same domain each time)
- Can configure **Cloudflare Access** for authentication
- Enterprise features available (WAF, DDoS protection, etc.)
- Suitable for production/long-term use

## Technical Details

### How It Works
1. `rccremote` spawns `cloudflared` as a child process
2. `cloudflared` establishes tunnel to Cloudflare edge
3. Cloudflare assigns a public URL (Quick) or uses configured domain (Named)
4. `rccremote` parses the URL from `cloudflared` stderr output
5. Traffic flows: Internet → Cloudflare → tunnel → localhost:4653

### Process Management
- Tunnel starts automatically with `--expose` flag
- Tunnel stops gracefully when rccremote exits (Ctrl+C)
- Uses context cancellation for clean shutdown

## References

- **Cloudflare Quick Tunnels:** https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/
- **Cloudflared Releases:** https://github.com/cloudflare/cloudflared/releases
- **Named Tunnels Setup:** https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/

## Related Commands

```bash
# Show rccremote version
rccremote --version

# Show all rccremote flags
rccremote --help

# Start with debug logging
rccremote --expose --debug

# Bind to specific hostname (with tunnel)
rccremote --expose --hostname 0.0.0.0 --port 4653
```
