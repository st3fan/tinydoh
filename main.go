package main

import (
	"encoding/base64"
	"net"
	"net/http"
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

func queryHandler(w http.ResponseWriter, r *http.Request) {
	println("Received a query")
	dns := r.URL.Query().Get("dns")
	if dns == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	query, err := base64.RawURLEncoding.DecodeString(dns)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

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
