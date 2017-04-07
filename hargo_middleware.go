package hargo_middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/mrichman/hargo"

	_ "github.com/lib/pq"
)

type LoggedRequest struct {
	ID      int    `db:"id"`
	Request string `db:"request"`
}

func LogHTTPMiddleware(next http.Handler, conn *sqlx.DB) http.HandlerFunc {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		headers := []hargo.NVP{}
		queryString := []hargo.NVP{}

		for k, vs := range r.Header {
			for _, v := range vs {
				headers = append(headers, hargo.NVP{Name: k, Value: v})
			}
		}

		for k, vs := range r.URL.Query() {
			for _, v := range vs {
				queryString = append(queryString, hargo.NVP{Name: k, Value: v})
			}
		}

		postBody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("[httpLogger] Error while decoding response body: err:", err)
		}

		hdrs := bytes.NewBuffer([]byte{})
		err = r.Header.WriteSubset(hdrs, nil)
		if err != nil {
			log.Println("[httpLogger] Error calculating length of headers: err:", err)
		}

		req := &hargo.Request{
			Method:      r.Method,
			URL:         r.URL.String(),
			HTTPVersion: r.Proto,
			Headers:     headers,
			QueryString: queryString,
			PostData: hargo.PostData{
				MimeType: r.Header.Get("Content-Type"),
				Text:     string(postBody),
			},
			HeaderSize: len(hdrs.Bytes()),
			BodySize:   len(postBody),
		}

		out, err := json.Marshal(req)
		if err != nil {
			log.Println("[httpLogger] Error encoding hargo request: err:", err)
		}

		go func() {
			stmt := "insert into requestlog (request) values (:request)"
			_, err := conn.NamedExec(stmt, LoggedRequest{Request: string(out)})
			if err != nil {
				log.Println("[httpLogger] Error writing logged request to DB: err:", err)
			}
		}()

		next.ServeHTTP(rw, r)
	})
}

func InitDatabase(connectionString string) *sqlx.DB {
	db, err := sqlx.Open("postgres", connectionString)
	if err != nil {
		panic(err)
	}

	db.MustExec(`CREATE TABLE IF NOT EXISTS requestlog (id SERIAL PRIMARY KEY, request json NOT NULL);`)
	return db
}

func handleFunc(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

func main() {
	connectionString := os.Getenv("DATABASE_URL")
	if connectionString == "" {
		connectionString = "postgres://localhost:5432?sslmode=disable"
	}

	db, err := sqlx.Open("postgres", connectionString)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	db.MustExec(`CREATE TABLE IF NOT EXISTS requestlog (id SERIAL PRIMARY KEY, request json NOT NULL);`)
	// Convert handlerFunc into http.Handler
	handler := http.HandlerFunc(handleFunc)

	// Set middleware
	http.HandleFunc("/", LogHTTPMiddleware(handler, db))
	fmt.Println("Listening on port :8000 ...")
	err = http.ListenAndServe(":8000", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}

}
