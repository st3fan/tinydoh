# Minimal DNS-Over-HTTPS Server

*Stefan Arentz, April 2018*

This is a tiny and minimal implementation of [draft-ietf-doh-dns-over-https](https://datatracker.ietf.org/doc/draft-ietf-doh-dns-over-https). It only supports `GET` requests and it is hardcoded to forward incoming DNS requests to `127.0.0.1:53`.

To use this in Firefox, you will have to deploy this to a HTTPS server. I use Caddy, with a config like this:

```
my.home.server.com {
    root /var/www
    gzip
    tls you@yourdomain.com

    proxy /dns-query 127.0.0.1:9091 {
          transparent
    }
}
```

I then run the server in a *tmux* session simply with `go run main.go`. This is obviously not production ready, it is an experiment / exploration.

To get this going in Firefox, you need the following:

* Firefox Nightly (Or possibly Firefox 60 Beta or later, not sure)
* Set `network.trr.url` to your `https://my.home.server.com/dns-query`
* Set `network.trr.mode` to something higher than 1 (See [TRR Preferences](https://gist.github.com/bagder/5e29101079e9ac78920ba2fc718aceec))
* Set `network.trr.useGET` to `true`

I had to restart Firefox before it picked up these settings.

