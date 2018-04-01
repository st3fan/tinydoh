// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/miekg/dns"
)

func (s *server) lookup(query []byte) ([]byte, error) {
	conn, err := net.DialUDP("udp", nil, s.upstream)
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(s.timeout))
	defer cancel()

	if d, ok := ctx.Deadline(); ok && !d.IsZero() {
		conn.SetDeadline(d)
	}

	if _, err := conn.Write(query); err != nil {
		return nil, err
	}

	buffer := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, err
	}

	return buffer[:n], nil
}

func parseHostFromQuery(query []byte) string {
	msg := &dns.Msg{}
	if err := msg.Unpack(query); err != nil {
		return "<unpack-error>"
	}
	s := ""
	for i := 0; i < len(msg.Question); i++ {
		if len(s) != 0 {
			s += ", "
		}
		n := msg.Question[i].Name
		c := dns.Class(msg.Question[i].Qclass).String()
		t := dns.Type(msg.Question[i].Qtype).String()
		s += fmt.Sprintf("%s/%s/%s", n, c, t)
	}
	return s
}

type server struct {
	verbose  bool
	upstream *net.UDPAddr
	timeout  time.Duration
}

func (s *server) queryHandler(w http.ResponseWriter, r *http.Request) {
	var query []byte

	switch r.Method {
	case "GET":
		encoded := r.URL.Query().Get("dns")
		if encoded == "" {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		dns, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		query = dns
	case "POST":
		if r.Header.Get("Content-Type") != "application/dns-udpwireformat" {
			http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
			return
		}

		defer r.Body.Close()

		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		query = body
	default:
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	start := time.Now()
	response, err := s.lookup(query)
	elapsed := time.Now().Sub(start)

	if err != nil {
		if e, ok := err.(net.Error); ok && e.Timeout() {
			if s.verbose {
				log.Printf("%s <%s> (timeout)\n", r.Method, parseHostFromQuery(query))
			}
			http.Error(w, http.StatusText(http.StatusRequestTimeout), http.StatusRequestTimeout)
		} else if err != nil {
			if s.verbose {
				log.Printf("%s <%s> (%s) %s\n", r.Method, parseHostFromQuery(query), elapsed, err.Error())
			}
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}

	if s.verbose {
		log.Printf("%s <%s> (%s)\n", r.Method, parseHostFromQuery(query), elapsed)
	}

	w.Header().Set("Content-Type", "application/dns-udpwireformat")
	w.Write(response)
}

func main() {
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	upstream := flag.String("upstream", "127.0.0.1:53", "upstream dns server")
	timeout := flag.Duration("timeout", 2500*time.Millisecond, "query timeout")
	flag.Parse()

	addr, err := net.ResolveUDPAddr("udp", *upstream)
	if err != nil {
		log.Fatalf("Failed to lookup upstream dns server: %s\n", err)
	}

	srv := &server{
		verbose:  *verbose,
		upstream: addr,
		timeout:  *timeout,
	}

	http.HandleFunc("/dns-query", srv.queryHandler)
	if err := http.ListenAndServe(":9091", nil); err != nil {
		log.Fatalf("Failed to start web server: %s\n", err)
	}
}
