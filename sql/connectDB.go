package db

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5"
)

var Connection *pgx.Conn

type DBConnection struct{}

func (db *DBConnection) Connect(db_credentials string){
	conn, err := pgx.Connect(context.Background(), db_credentials)

	if err != nil {
		log.Fatal("- Error connecting with the DB", err)
	}

	Connection = conn
}


func GetDB() *pgx.Conn {
	return Connection
}