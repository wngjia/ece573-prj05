package main

import (
	"log"
	"net/http"
)

func main() {
	svc, err := openService()
	if err != nil {
		log.Fatal("FATAL open login service: ", err)
	}
	defer svc.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /accounts", createAccountHandler(svc))
	mux.HandleFunc("POST /login", loginHandler(svc))
	log.Println("INFO login service listening on :8081")
	log.Fatal("FATAL login HTTP server: ", http.ListenAndServe(":8081", mux))
}
