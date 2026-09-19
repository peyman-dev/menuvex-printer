use std::{ net::{ Ipv4Addr, SocketAddr, TcpStream }, io::Write, time::{ Duration, Instant } };
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
}
