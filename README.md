# Minimal DNS-Over-HTTPS Server

*Stefan Arentz, April 2018*

This is a tiny and minimal implementation of [draft-ietf-doh-dns-over-https](https://datatracker.ietf.org/doc/draft-ietf-doh-dns-over-https).

By default it forwards incoming DNS requests to `127.0.0.1:53`. This means you need to have a DNS server running on the machine where you run this service. Personally I like `apt-get install pdns-recursor`. You can also use the `-upstream` argument to use a different dns server.
 
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

I then run the server in a *tmux* session simply with `go run main.go -verbose`. This is obviously not production ready, it is an experiment / exploration.

To get this going in Firefox, you need the following:

* Firefox Nightly (Or possibly Firefox 60 Beta or later, not sure)
* Set `network.trr.url` to your `https://my.home.server.com/dns-query`
* Set `network.trr.mode` to something higher than 1 (See [TRR Preferences](https://gist.github.com/bagder/5e29101079e9ac78920ba2fc718aceec))

I had to restart Firefox before it picked up these settings. You should see something like this appear:

```
2018/03/31 13:47:31 POST Request for <golang.org./IN/A> (592.183µs)
2018/03/31 13:47:31 POST Request for <golang.org./IN/AAAA> (2.513745ms)
2018/03/31 13:47:31 POST Request for <golang.org./IN/A> (812.055µs)
2018/03/31 13:47:31 POST Request for <golang.org./IN/AAAA> (787.912µs)
2018/03/31 13:47:48 POST Request for <blog.golang.org./IN/AAAA> (206.335515ms)
2018/03/31 13:47:49 POST Request for <blog.golang.org./IN/A> (237.966346ms)
```

Enjoy.
