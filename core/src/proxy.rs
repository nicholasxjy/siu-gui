//! Persistent download proxy policy. System routing is re-read for each request.

use std::fs;
use std::path::Path;
use std::process::Command;

use anyhow::{Context, Result, bail};
use proxy_watch::{ProxyConfig, ProxyEnv, ProxyStep, Scheme, Url};
use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Mode {
    #[default]
    System,
    Custom,
    Direct,
}

#[derive(Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Settings {
    #[serde(default)]
    pub mode: Mode,
    #[serde(default)]
    pub url: String,
    #[serde(default)]
    pub no_proxy: String,
}

impl Settings {
    fn validate(mut self) -> Result<Self> {
        self.url = self.url.trim().to_string();
        self.no_proxy = self.no_proxy.trim().to_string();
        if self.url.len() > 4096 || self.no_proxy.len() > 8192 {
            bail!("代理设置过长，请缩短代理地址或绕过列表。");
        }
        if self.mode == Mode::Custom {
            let url =
                Url::parse(&self.url).map_err(|_| anyhow::anyhow!("请输入有效的代理地址。"))?;
            if !matches!(url.scheme(), "http" | "https" | "socks5" | "socks5h")
                || url.host_str().is_none()
                || url.port() == Some(0)
                || !matches!(url.path(), "" | "/")
                || url.query().is_some()
                || url.fragment().is_some()
            {
                bail!("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，且不能包含路径、查询或片段。");
            }
            // Validate with the same parser used to route actual downloads.
            let env = self.custom_env()?;
            if !env.rejected().is_empty() || env.endpoint_for(Scheme::Https).is_none() {
                bail!("请输入有效的代理服务器地址和端口。");
            }
            if !env.bypass().rejected.is_empty() {
                bail!("绕过列表包含无效项目，请使用逗号分隔的域名、IP 或 CIDR。");
            }
        }
        Ok(self)
    }

    fn custom_env(&self) -> Result<ProxyEnv> {
        ProxyEnv::from_vars([
            ("all_proxy", self.url.as_str()),
            ("no_proxy", self.no_proxy.as_str()),
        ])
        .context("无法解析手动代理设置")
    }
}

pub fn load(root: &Path) -> Result<Settings> {
    let path = root.join("proxy.json");
    match fs::read(&path) {
        Ok(bytes) => {
            let settings: Settings = serde_json::from_slice(&bytes)
                .map_err(|_| anyhow::anyhow!("无法读取代理设置，请在设置页重新保存。"))?;
            settings.validate()
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Settings::default()),
        Err(e) => Err(e).context("无法读取代理设置"),
    }
}

pub fn current() -> Result<Settings> {
    load(&crate::env::resolve().root)
}

pub fn save(root: &Path, settings: Settings) -> Result<Settings> {
    let settings = settings.validate()?;
    crate::mcp::write_atomic(
        &root.join("proxy.json"),
        &serde_json::to_vec_pretty(&settings)?,
    )
    .context("无法保存代理设置")?;
    Ok(settings)
}

/// NO_PROXY applies to OS proxies too. Explicit protocol/ALL_PROXY variables
/// override the OS; a NO_PROXY variable alone does not disable the system proxy.
fn system_config(url: &Url) -> Result<ProxyConfig> {
    let env = ProxyEnv::from_env().context("无法读取代理环境变量")?;
    select_system_config(url, &env, proxy_watch::read)
}

fn select_system_config(
    url: &Url,
    env: &ProxyEnv,
    read_system: impl FnOnce() -> std::result::Result<ProxyConfig, proxy_watch::Error>,
) -> Result<ProxyConfig> {
    if env.bypass().matches_url(url) {
        return Ok(ProxyConfig::direct());
    }
    if let Some(scheme) = Scheme::from_name(url.scheme())
        && env.to_mode().entry_for(scheme).is_some()
    {
        return Ok(env.to_config());
    }
    match read_system() {
        Ok(config) => Ok(config),
        Err(proxy_watch::Error::Unsupported) => Ok(env.to_config()),
        Err(e) => Err(e).context("无法读取系统代理，请在设置中手动配置代理或选择直连"),
    }
}

fn resolve_config(config: &ProxyConfig, url: &Url) -> Result<Vec<ProxyStep>> {
    match proxy_watch::resolve(config, url) {
        Ok(steps) => Ok(steps),
        Err(proxy_watch::Error::PacNotSupported { .. }) => {
            use proxy_watch::pac::{PacPolicy, PacResolver};
            let resolver = PacResolver::new(PacPolicy::default()).with_system_native()?;
            #[cfg(target_os = "macos")]
            let resolver =
                resolver.with_native(proxy_watch::pac::NativePacResolver::new().with_wpad(true));
            #[cfg(windows)]
            let resolver =
                resolver.with_native(proxy_watch::pac::NativePacResolver::new()?.with_wpad(true));
            resolver
                .resolve_config(config, url, None)
                .context("无法解析系统自动代理，请在设置中手动配置代理")
        }
        Err(e) => Err(e).context("无法解析系统代理"),
    }
}

pub fn routes(settings: &Settings, url: &Url) -> Result<Vec<Option<ureq::Proxy>>> {
    let config = match settings.mode {
        Mode::Direct => return Ok(vec![None]),
        Mode::Custom => settings.custom_env()?.to_config(),
        Mode::System => system_config(url)?,
    };
    resolve_config(&config, url)?
        .iter()
        .map(proxy_from_step)
        .collect()
}

fn proxy_from_step(step: &ProxyStep) -> Result<Option<ureq::Proxy>> {
    use proxy_watch::ProxyScheme;
    use ureq::ProxyProtocol;
    let Some(endpoint) = step.endpoint() else {
        return Ok(None);
    };
    let protocol = match step {
        ProxyStep::Http(_) => ProxyProtocol::Http,
        ProxyStep::Https(_) => ProxyProtocol::Https,
        ProxyStep::Socks4(_) if endpoint.scheme_hint == Some(ProxyScheme::Socks4a) => {
            ProxyProtocol::Socks4A
        }
        ProxyStep::Socks4(_) => ProxyProtocol::Socks4,
        ProxyStep::Socks5(_) if endpoint.scheme_hint == Some(ProxyScheme::Socks5h) => {
            ProxyProtocol::Socks5h
        }
        ProxyStep::Socks5(_) => ProxyProtocol::Socks5,
        _ => bail!("不支持此系统代理协议，请手动配置 HTTP 或 SOCKS5 代理。"),
    };
    let mut builder = ureq::Proxy::builder(protocol)
        .host(&endpoint.host.to_string())
        .port(endpoint.port);
    if let Some(auth) = &endpoint.auth {
        builder = builder.username(auth.username());
        if let Some(password) = auth.password() {
            builder = builder.password(password);
        }
    }
    Ok(Some(builder.build().context("无法创建下载代理")?))
}

/// Child tools inherit explicit proxy policy rather than stale launch-time
/// variables. Docker's daemon still owns image transfers and its own proxy.
pub fn configure_command(command: &mut Command) -> Result<()> {
    let settings = current()?;
    for (scheme, destination) in [
        ("HTTP", "http://registry-1.docker.io/"),
        ("HTTPS", "https://registry-1.docker.io/"),
    ] {
        let proxy = routes(&settings, &Url::parse(destination)?)?
            .into_iter()
            .next()
            .flatten();
        let value = proxy.map(|p| p.uri().to_string()).unwrap_or_default();
        command
            .env(format!("{scheme}_PROXY"), &value)
            .env(format!("{}_proxy", scheme.to_lowercase()), &value);
    }
    // Protocol-specific settings take precedence; an inherited ALL_PROXY
    // must never override an explicit direct choice.
    command.env_remove("ALL_PROXY").env_remove("all_proxy");
    match settings.mode {
        Mode::Direct => {
            command.env("NO_PROXY", "*").env("no_proxy", "*");
        }
        Mode::Custom => {
            command
                .env("NO_PROXY", &settings.no_proxy)
                .env("no_proxy", &settings.no_proxy);
        }
        Mode::System => {}
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn custom(url: &str, no_proxy: &str) -> Settings {
        Settings {
            mode: Mode::Custom,
            url: url.into(),
            no_proxy: no_proxy.into(),
        }
    }

    #[test]
    fn defaults_persist_and_validate_without_overwriting_good_settings() {
        let home = tempfile::tempdir().unwrap();
        assert!(load(home.path()).unwrap().mode == Mode::System);
        let settings = custom(
            " socks5h://Alice:SeCrEt@127.0.0.1:1080 ",
            "localhost,*.local,10.0.0.0/8",
        );
        let saved = save(home.path(), settings).unwrap();
        assert!(load(home.path()).unwrap() == saved);
        for url in [
            "",
            "not a URL",
            "ftp://localhost:21",
            "http://localhost:0",
            "http://localhost:80/path",
            "http://localhost:80?password=secret",
        ] {
            assert!(save(home.path(), custom(url, "")).is_err(), "{url}");
            assert!(load(home.path()).unwrap() == saved);
        }
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(
                fs::metadata(home.path().join("proxy.json"))
                    .unwrap()
                    .permissions()
                    .mode()
                    & 0o777,
                0o600
            );
        }
    }

    #[test]
    fn custom_credentials_bypass_and_direct_are_honored() {
        let settings = custom(
            "socks5h://Alice:Pa%40sS@127.0.0.1:1080",
            "localhost,*.local,10.0.0.0/8",
        )
        .validate()
        .unwrap();
        let proxy = routes(&settings, &Url::parse("https://github.com/a").unwrap())
            .unwrap()
            .remove(0)
            .unwrap();
        assert_eq!(proxy.username(), Some("Alice"));
        assert_eq!(proxy.password(), Some("Pa@sS"));
        assert!(!proxy.resolve_target());
        for address in [
            "http://localhost/x",
            "https://host.local",
            "http://10.2.3.4/x",
        ] {
            assert!(routes(&settings, &Url::parse(address).unwrap()).unwrap()[0].is_none());
        }
        let direct = Settings {
            mode: Mode::Direct,
            ..settings
        };
        assert!(routes(&direct, &Url::parse("https://github.com").unwrap()).unwrap()[0].is_none());
    }

    #[test]
    fn environment_proxy_precedence_and_no_proxy_only_keep_system_routing() {
        let url = Url::parse("https://github.com/x").unwrap();
        let os = || {
            Ok(
                ProxyEnv::from_vars([("all_proxy", "http://127.0.0.1:7890")])
                    .unwrap()
                    .to_config(),
            )
        };
        let no_proxy = ProxyEnv::from_vars([("no_proxy", "localhost,127.0.0.1")]).unwrap();
        let config = select_system_config(&url, &no_proxy, os).unwrap();
        assert_eq!(
            config.effective.endpoint_for(Scheme::Https).unwrap().port,
            7890
        );
        let env = ProxyEnv::from_vars([("all_proxy", "socks5h://127.0.0.1:1080")]).unwrap();
        let config = select_system_config(&url, &env, || panic!("explicit env must win")).unwrap();
        assert_eq!(
            config.effective.endpoint_for(Scheme::Https).unwrap().port,
            1080
        );
        let config = select_system_config(
            &Url::parse("http://localhost/x").unwrap(),
            &no_proxy,
            || panic!("bypassed URL"),
        )
        .unwrap();
        assert!(config.effective.is_direct());
    }
}
