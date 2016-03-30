# go-websockproxy

This is a Go port of [websockproxy](https://github.com/benjamincburns/websockproxy) licensed under [GNU/GPLv2](./LICENSE).
It is a websockets server that allows network support in [jor1k](https://github.com/s-macke/jor1k) OR1K emulators running Linux.

Pull requests are welcome.

## Features
- [x] client authentication
- [x] secure websockets (TLS a.k.a. `wss://`)
- [x] MAC prefix whitelisting
- [x] download/upload rate limiting
- [x] serving a directory with static files
- [ ] re-attaching persistent TAP interfaces (would be handy for non-root usage)

```
Usage of bin/go-websockproxy:
  --auth-key string
    	accept TAP traffic via websockets only if authorized with this key; by default is disabled (accepts any traffic)
  --cert-file string
    	certificate for listening on TLS connections; by default TLS is disabled
  --key-file string
    	key file for listening on TLS connections; by default TLS is disabled
  --listen-address string
    	address to listen on for incoming websocket connections; URI is '/wstap' (default ":8000")
  --log-level string
    	one of 'debug', 'info', 'warning', 'error' (default "warning")
  --mac-prefix string
    	accept websockets traffic only with MACs starting with the specified prefix (default is disabled)
  --max-download-bandwidth string
    	max upload bandwidth per client; leave empty for unlimited
  --max-upload-bandwidth string
    	max upload bandwidth per client; leave empty for unlimited
  --static-directory string
    	static files directory to serve at '/'; disabled by default
  --tap-ipv4 string
    	IPv4 address for the TAP interface; used only when interface is created (default "10.3.0.1/16")
```

go-websockproxy would by default be accessible at `wss://localhost:8000/wstap`.

In order to use features like AUTH key, query-specified relay URL and MAC address whitelisting, give a peek to [author's fork of jor1k](https://github.com/gdm85/jor1k/).

# Building

Repository's submodules should be initialised:
```
git submodule update --init --recursive
```

Build with:

```
make
```

Then you can run:
```
bin/go-websockproxy
```

# Example usage (as root)

A simple command-line:
```
bin/go-websockproxy --listen-address=:8080 --tap-ipv4=10.5.0.1/16 --static-directory=jor1k --max-download-bandwidth=50kbps --max-upload-bandwidth=50kbps --log-level=info
```

A more complex command-line:
```
bin/go-websockproxy --cert-file=mycert.pem --key-file=mycert.key --mac-prefix="00:15 --auth-key="yoursecrethere"  --max-download-bandwidth=50kbps --max-upload-bandwidth=50kbps --log-level=debug"
```

Once go-websockproxy is started, you may want to start a DHCP server as in:
```
dnsmasq -d --bind-interfaces --listen-address=10.3.0.1 --dhcp-range=10.3.0.50,10.3.0.200,12h --dhcp-option=option:router,10.3.0.1 --dhcp-option=option:dns-server,10.3.0.1 --log-dhcp
```

Additionally, IPv4 forwarding should be enabled if you want to allow the jor1k clients to connect to regular internet.

To quickly generate a TLS certificate + key pair: https://golang.org/src/crypto/tls/generate_cert.go

# License

[GNU/GPLv2](./LICENSE)

# Thanks

Thanks to Benjamin Burns for the python version upon which I based this one and to Sebastian Macke for the great jor1k.
