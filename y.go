// +build ignore

package main

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"sync"
)

func main() {
	db, err := sql.Open("sqlite3", "file:locked.sqlite?cache=shared&mode=rwc")
	if err != nil {
		log.Fatalln("could not open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("DROP TABLE IF EXISTS test")
	if err != nil {
		log.Fatalln("could not drop table:", err)
	}
	_, err = db.Exec(`CREATE TABLE test (
		"Id" integer not null primary key autoincrement,
		"Hash" blob,
		"Vary" varchar(255),
		"VaryHash" blob,
		"StatusCode" integer,
		"ModTime" integer,
		"DownloadTimeNano" integer,
		"ExpiryTime" integer,
		"LastUsedTime" integer,
		"ETag" varchar(255),
		"ContentLength" integer
	)`)
	if err != nil {
		log.Fatalln("could not create table:", err)
	}

	_, err = db.Exec("DROP TABLE IF EXISTS log")
	if err != nil {
		log.Fatalln("could not drop table:", err)
	}
	_, err = db.Exec(`CREATE TABLE log (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		StatusCode INTEGER,
		ModTime INTEGER
	)`)
	if err != nil {
		log.Fatalln("could not create table:", err)
	}

	q := &sync.WaitGroup{}
	nChildren := 40
	q.Add(nChildren)
	for i := 0; i < nChildren; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				rows, err := db.Query(
					"SELECT * FROM test WHERE ModTime=?", i)
				if err != nil {
					log.Fatalln("select failed:", err)
				}
				err = rows.Close()
				if err != nil {
					log.Fatalln("Rows.Close() failed:", err)
				}
				res, err := db.Exec(
					"INSERT INTO test (StatusCode, ModTime) VALUES (?, ?)", i, -1)
				if err != nil {
					log.Fatalln("could not insert into table:", err)
				}
				id, err := res.LastInsertId()
				if err != nil {
					log.Fatalln("LastInsertId:", err)
				}
				_, err = db.Exec(
					"UPDATE test SET ModTime=? WHERE id=?", j, id)
				if err != nil {
					log.Fatalln("could not update table:", err)
				}
				_, err = db.Exec(
					"INSERT INTO log (StatusCode, ModTime) VALUES (?, ?)", i, j)
				if err != nil {
					log.Fatalln("could not insert into log:", err)
				}
			}
			q.Done()
		}()
	}
	q.Wait()
}
