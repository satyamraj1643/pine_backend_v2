package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"github.com/joho/godotenv"
	"github.com/satyamraj1643/pine_backend_v2/handlers"
	db "github.com/satyamraj1643/pine_backend_v2/sql"
)

func main() {
	http.HandleFunc("/", handlers.HandleRequest)

	port := ":8080"

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}


	dbSecret := os.Getenv("DATABASE_URL")
	if dbSecret == "" {
		log.Fatal("DATABASE_URL not set in environment variables")
	}


	dbConn := &db.DBConnection{}
	dbConn.Connect(dbSecret)

	fmt.Printf("Server is running on port %s\n", port)

	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatal("Error starting the pine backend systems:", err)
	}
}
