// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"github.com/miekg/dns"
)

func lookup(query []byte) ([]byte, error) {
	serverAddress, err := net.ResolveUDPAddr("udp", "127.0.0.1:53")
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, serverAddress)
	if err != nil {
		return nil, err
	}

	defer conn.Close()

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

func queryHandler(w http.ResponseWriter, r *http.Request) {
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

	log.Printf("%s Request for <%s>\n", r.Method, parseHostFromQuery(query))

	response, err := lookup(query)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/dns-udpwireformat")
	w.Write(response)
}

func main() {
	http.HandleFunc("/dns-query", queryHandler)
	if err := http.ListenAndServe(":9091", nil); err != nil {
		panic(err)
	}
}
