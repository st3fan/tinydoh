// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/domainr/dnsr"
	"github.com/miekg/dns"
)

type server struct {
	verbose  bool
	upstream *net.UDPAddr
	timeout  time.Duration
	resolver *dnsr.Resolver
}

// a global resolver only used by lambda
var resolver *dnsr.Resolver

func init() {
	// when running as lambda, create the resolver in init with a
	// fixed cache size to have it persist between runs
	if os.Getenv("LAMBDA_TASK_ROOT") != "" {
		resolver = dnsr.NewWithTimeout(1000000, 2500*time.Millisecond)
	}
}

func (s *server) upstreamLookup(query string) ([]byte, error) {
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

	if _, err := conn.Write([]byte(query)); err != nil {
		return nil, err
	}

	buffer := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, err
	}

	return buffer[:n], nil
}

func (s *server) queryHandler(w http.ResponseWriter, r *http.Request) {
	var (
		err      error
		n, t     string
		response dns.Msg
		packed   []byte
		elapsed  time.Duration
	)

	switch r.Method {
	case "GET":
		encoded := r.URL.Query().Get("dns")
		if encoded == "" {
			log.Println("missing dns query parameter in GET request")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		n = string(decoded)
		t = "A"
	case "POST":
		if r.Header.Get("Content-Type") != "application/dns-udpwireformat" {
			http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
			return
		}
		defer r.Body.Close()
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		// parse the dns message
		msg := &dns.Msg{}
		if err := msg.Unpack(body); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if len(msg.Question) != 1 {
			log.Println("DoH only supports single queries")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		n = msg.Question[0].Name
		t = dns.Type(msg.Question[0].Qtype).String()
	default:
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// resolve the query
	start := time.Now()
	if s.upstream != nil {
		// resolve the query using an upstream resolver
		packed, err = s.upstreamLookup(n)
		elapsed = time.Now().Sub(start)

		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				if s.verbose {
					log.Printf("%s <%s/%s> (timeout)\n", r.Method, n, t)
				}
				http.Error(w, http.StatusText(http.StatusRequestTimeout), http.StatusRequestTimeout)
			} else if err != nil {
				if s.verbose {
					log.Printf("%s <%s/%s> (%s) %s\n", r.Method, n, t, elapsed, err.Error())
				}
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			return
		}
	} else {
		// resolve the query using the internal resolver
		rrs, err := s.resolver.ResolveErr(n, t)
		elapsed = time.Now().Sub(start)
		if err == dnsr.NXDOMAIN {
			err = nil
		}
		if err != nil {
			log.Printf("%s Request for <%s/%s> %s\n", r.Method, n, t, err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		for _, rr := range rrs {
			newRR, err := dns.NewRR(rr.String())
			if err != nil {
				log.Println(err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			response.Answer = append(response.Answer, newRR)
		}
		packed, err = response.Pack()
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}
	if s.verbose {
		log.Printf("%s Request for <%s/%s> (%s)\n", r.Method, n, t, elapsed.String())
	}
	w.Header().Set("Content-Type", "application/dns-udpwireformat")
	w.Write(packed)
}

// LambdaHandler processes the request using the AWS Lambda framework. It only supports the internal
// resolver and has a fixed cache size.
func LambdaHandler(r events.APIGatewayProxyRequest) (resp events.APIGatewayProxyResponse, err error) {
	var (
		n, t     string
		response dns.Msg
		packed   []byte
		elapsed  time.Duration
	)
	switch r.HTTPMethod {
	case "GET":
		if r.QueryStringParameters["dns"] == "" {
			err = errors.New("missing dns query parameter in GET request")
			log.Println(err)
			return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: err.Error()}, err
		}
		decoded, err := base64.RawURLEncoding.DecodeString(r.QueryStringParameters["dns"])
		if err != nil {
			log.Println(err)
			return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: err.Error()}, err
		}
		n = string(decoded)
		t = "A"
	case "POST":
		if r.Headers["content-type"] != "application/dns-udpwireformat" {
			err = errors.New("unsupported media type, expected application/dns-udpwireformat")
			log.Println(err)
			return events.APIGatewayProxyResponse{StatusCode: http.StatusUnsupportedMediaType, Body: err.Error()}, err
		}
		// parse the dns message
		msg := &dns.Msg{}
		if err := msg.Unpack([]byte(r.Body)); err != nil {
			log.Println(err)
			return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: err.Error()}, err
		}
		if len(msg.Question) != 1 {
			err = errors.New("DoH only supports single queries")
			log.Println(err)
			return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: err.Error()}, err
		}
		n = msg.Question[0].Name
		t = dns.Type(msg.Question[0].Qtype).String()
	default:
		err = errors.New("unsupported http method")
		log.Println(err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: err.Error()}, err
	}

	// resolve the query
	start := time.Now()
	// resolve the query using the internal resolver
	rrs, err := resolver.ResolveErr(n, t)
	elapsed = time.Now().Sub(start)
	if err == dnsr.NXDOMAIN {
		err = nil
	}
	if err != nil {
		log.Printf("%s Request for <%s/%s> %s\n", r.HTTPMethod, n, t, err.Error())
		return events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError, Body: err.Error()}, err
	}

	for _, rr := range rrs {
		newRR, err := dns.NewRR(rr.String())
		if err != nil {
			log.Println(err)
			return events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError, Body: err.Error()}, err
		}

		response.Answer = append(response.Answer, newRR)
	}
	packed, err = response.Pack()
	if err != nil {
		log.Println(err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError, Body: err.Error()}, err
	}
	log.Printf("%s Request for <%s/%s> (%s)\n", r.HTTPMethod, n, t, elapsed.String())
	resp.Headers = make(map[string]string)
	resp.Headers["Content-Type"] = "application/dns-udpwireformat"
	resp.StatusCode = http.StatusOK
	resp.Body = string(packed)
	return
}

func main() {
	if os.Getenv("LAMBDA_TASK_ROOT") != "" {
		lambda.Start(LambdaHandler)
	} else {
		verbose := flag.Bool("verbose", false, "enable verbose logging")
		capacity := flag.Int("capacity", 1000000, "capacity of the resolver cache")
		upstream := flag.String("upstream", "", "upstream dns server (eg. '127.0.0.1:53'). If not set, use internal resolver.")
		timeout := flag.Duration("timeout", 2500*time.Millisecond, "query timeout")
		flag.Parse()

		srv := &server{
			verbose:  *verbose,
			resolver: dnsr.NewWithTimeout(*capacity, *timeout),
			timeout:  *timeout,
		}
		if *upstream != "" {
			addr, err := net.ResolveUDPAddr("udp", *upstream)
			if err != nil {
				log.Fatalf("Failed to lookup upstream dns server: %s\n", err)
			}
			srv.upstream = addr
		}

		http.HandleFunc("/dns-query", srv.queryHandler)
		if err := http.ListenAndServe(":9091", nil); err != nil {
			log.Fatalf("Failed to start web server: %s\n", err)
		}
	}
}
