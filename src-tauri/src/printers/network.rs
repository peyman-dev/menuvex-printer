use std::{
    net::{ Ipv4Addr, SocketAddr, TcpStream, UdpSocket },
    io::Write,
    sync::{
        atomic::{ AtomicUsize, Ordering },
        Mutex,
    },
    thread,
    time::{ Duration, Instant },
};
use serde::Serialize;
use crate::error::{ AgentError, Result };
pub fn address(host: &str, port: u16) -> Result<SocketAddr> {
    let ip: Ipv4Addr = host
        .parse()
        .map_err(|_|
            AgentError::new(
                "INVALID_CONFIG",
                "Use a literal private IPv4 address; DNS is not allowed"
            )
        )?;
    // No public, localhost, link-local, multicast or metadata endpoints. Port changes require local UI.
    if !ip.is_private() || port == 0 || ip.octets()[3] == 0 || ip.octets()[3] == 255 {
        return Err(
            AgentError::new(
                "INVALID_CONFIG",
                "Only private LAN unicast IPv4 addresses are supported"
            )
        );
    }
    Ok(SocketAddr::new(ip.into(), port))
}
fn connect(host: &str, port: u16) -> Result<TcpStream> {
    TcpStream::connect_timeout(&address(host, port)?, Duration::from_secs(3)).map_err(|e|
        AgentError::retry(
            if e.kind() == std::io::ErrorKind::TimedOut {
                "NETWORK_TIMEOUT"
            } else {
                "NETWORK_CONNECTION_FAILED"
            }
        )
    )
}
pub fn probe(host: &str, port: u16) -> Result<()> {
    connect(host, port).map(|_| ())
}
pub fn send(host: &str, port: u16, bytes: &[u8]) -> Result<()> {
    let mut stream = connect(host, port)?;
    stream
        .set_write_timeout(Some(Duration::from_secs(3)))
        .map_err(|_| AgentError::retry("NETWORK_CONNECTION_FAILED"))?;
    let deadline = Instant::now() + Duration::from_secs(30);
    for chunk in bytes.chunks(16 * 1024) {
        if Instant::now() > deadline {
            return Err(AgentError::uncertain());
        }
        stream.write_all(chunk).map_err(|_| AgentError::uncertain())?;
    }
    stream.flush().map_err(|_| AgentError::uncertain())?;
    Ok(())
}

/// A printer candidate found by scanning the local network for an open RAW port.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DiscoveredNetwork {
    pub host: String,
    pub port: u16,
}

/// Best-effort local IPv4 without extra crates: a "connected" UDP socket never
/// sends packets, but the kernel still selects the outbound interface/source.
fn local_ipv4() -> Option<Ipv4Addr> {
    let socket = UdpSocket::bind((Ipv4Addr::UNSPECIFIED, 0)).ok()?;
    socket.connect((Ipv4Addr::new(8, 8, 8, 8), 80)).ok()?;
    match socket.local_addr().ok()? {
        SocketAddr::V4(v4) => Some(*v4.ip()),
        _ => None,
    }
}

/// Usable host addresses of the /24 that contains `base` (drops network/broadcast/self).
fn subnet_hosts(base: Ipv4Addr) -> Vec<Ipv4Addr> {
    let o = base.octets();
    (1..=254u8)
        .map(|last| Ipv4Addr::new(o[0], o[1], o[2], last))
        .filter(|ip| ip != &base)
        .collect()
}

fn scan(hosts: &[Ipv4Addr], port: u16) -> Vec<DiscoveredNetwork> {
    if hosts.is_empty() {
        return Vec::new();
    }
    let next = AtomicUsize::new(0);
    let found = Mutex::new(Vec::new());
    let workers = 64usize.min(hosts.len());
    thread::scope(|scope| {
        for _ in 0..workers {
            scope.spawn(|| {
                loop {
                    let i = next.fetch_add(1, Ordering::Relaxed);
                    let Some(&ip) = hosts.get(i) else {
                        break;
                    };
                    let addr = SocketAddr::new(ip.into(), port);
                    if TcpStream::connect_timeout(&addr, Duration::from_millis(400)).is_ok() {
                        found.lock().unwrap().push(DiscoveredNetwork {
                            host: ip.to_string(),
                            port,
                        });
                    }
                }
            });
        }
    });
    let mut out = found.into_inner().unwrap();
    out.sort_by(|a, b| a.host.cmp(&b.host));
    out
}

/// Scan the local subnet(s) for hosts accepting a RAW connection on port 9100.
/// Operator-initiated and passive; a hit is only a candidate until a test print
/// confirms it (same caveat as the manual probe: "reachable" is not "printed").
pub fn discover() -> Result<Vec<DiscoveredNetwork>> {
    const PORT: u16 = 9100;
    let mut bases = Vec::new();
    match local_ipv4() {
        Some(ip) => {
            let o = ip.octets();
            bases.push(Ipv4Addr::new(o[0], o[1], o[2], 0));
        }
        // Could not determine the interface; fall back to the most common LANs.
        None => {
            bases.push(Ipv4Addr::new(192, 168, 1, 0));
            bases.push(Ipv4Addr::new(192, 168, 0, 0));
        }
    }
    let mut hosts: Vec<Ipv4Addr> = Vec::new();
    for base in bases {
        for ip in subnet_hosts(base) {
            // Only ever probe targets that pass the same safety check as printing.
            if address(&ip.to_string(), PORT).is_ok() && !hosts.contains(&ip) {
                hosts.push(ip);
            }
        }
    }
    Ok(scan(&hosts, PORT))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_unsafe_targets() {
        for host in ["localhost", "127.0.0.1", "169.254.169.254", "8.8.8.8", "224.0.0.1", "::1"] {
            assert!(address(host, 9100).is_err());
        }
        assert!(address("192.168.1.50", 9100).is_ok());
        assert!(address("10.0.0.1", 0).is_err());
    }

    #[test]
    fn subnet_hosts_are_usable_private_addresses() {
        let hosts = subnet_hosts(Ipv4Addr::new(10, 0, 5, 0));
        assert_eq!(hosts.len(), 254);
        assert!(hosts.contains(&Ipv4Addr::new(10, 0, 5, 1)));
        assert!(hosts.contains(&Ipv4Addr::new(10, 0, 5, 254)));
        assert!(!hosts.contains(&Ipv4Addr::new(10, 0, 5, 0)));
        assert!(!hosts.contains(&Ipv4Addr::new(10, 0, 5, 255)));
        for ip in hosts {
            assert!(address(&ip.to_string(), 9100).is_ok());
        }
    }

    #[test]
    fn discover_only_reports_private_candidates() {
        // Bounded real scan; CI usually has no RAW printers, so this is often
        // empty. Whatever it returns must be a private port-9100 target.
        for found in discover().expect("discover should not error") {
            assert_eq!(found.port, 9100);
            assert!(address(&found.host, found.port).is_ok());
        }
    }
}
