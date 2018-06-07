package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	flag "github.com/ogier/pflag"
)

// Config keeps the configuration
var Config Configuration
var version = "development"

// Response describes the parts we want from cloudflare's json response
type Response struct {
	Summary struct {
		Hours []struct {
			Avgresponse int       `json:"avgresponse"`
			Downtime    int       `json:"downtime"`
			Starttime   Timestamp `json:"starttime"`
			Uptime      int       `json:"uptime"`
		} `json:"hours"`
	} `json:"summary"`
}

// init() runs before the main function as described in:
// https://golang.org/doc/effective_go.html#init
func init() {
	flag.StringVar(&Config.usermail, "email", "", "Pingdom's API configured e-mail account")
	flag.StringVar(&Config.pass, "pass", "", "password for pingdom's API")
	flag.StringVar(&Config.headerXappkey, "appkey", "", "Appkey for pingdom's API")
	// flag.StringVar(&Config.checkname, "checkname", "", "Name of the check (eg summary.performance)") //multiple checks seperated by comma?
	flag.StringVar(&Config.checkid, "checkid", "", "ID of the check, aka the domain are we checking.")
	flag.Int32Var(&Config.from, "from", int32(time.Now().Add(-24*time.Hour).Unix()), "from which (Unix)time we are asking, default 24 hours ago which is ")
	flag.Int32Var(&Config.to, "to", int32(time.Now().Unix()), "until which (Unix)time we are asking, default now which is ")
	flag.StringVar(&Config.output, "output", "console", "Output destination (console, db)")
	flag.StringVar(&Config.mysqlurl, "mysqlurl", "", "mysql connection in DSN, like: username:password@(address)/dbname.\n\tCannot use together with --pgurl")
	flag.StringVar(&Config.pgurl, "pgurl", "", "postgres connection in DSN, like: postgres://username:password@address:port/dbname?sslmode=disable.\n\tCannot use together with --mysqlurl")
	flag.StringVar(&Config.pgschema, "pgschema", "postgres", "Postgres schema")
	flag.BoolVar(&Config.inittable, "inittable", false, "Initialize the table, requires --mysqlurl ")
	flag.BoolVar(&Config.addcheck, "addcheck", false, "Add new check into the mysql table, requires a data store,--checkid ")

	flag.Usage = func() {
		fmt.Println("Using Pingdom's API as described in: https://www.pingdom.com/resources/api")
		fmt.Printf("Version: %s\nUsage: pingdom2stats [options]\nMost options are required (and some have defaults):\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()
	if Config.configurationInvalid() {
		flag.Usage()
		os.Exit(1)
	}
}

// Gets data from Pingdom's API
func getPingdomData() (*Response, error) {
	// make the request with the appropriate headers
	req, err := http.NewRequest("GET",
		fmt.Sprintf(
			"https://api.pingdom.com/api/2.0/summary.performance/%s?from=%d&to=%d&includeuptime=true",
			Config.checkid,
			Config.from,
			Config.to),
		nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(Config.usermail, Config.pass)
	req.Header.Set("app-key", Config.headerXappkey)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		fmt.Println(string(body))
		return nil, errors.New("API not 200")
	}
	var response Response
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}
	response.Summary.Hours = response.Summary.Hours[:len(response.Summary.Hours)-1]
	return &response, err
}

func consoleOutput(res *Response) error {
	for _, hour := range res.Summary.Hours {
		fmt.Println(hour.Starttime.Time, hour.Uptime, hour.Avgresponse, hour.Downtime)
	}
	return nil
}
func connectToDB() *sql.DB {
	//	db, err := sql.Open("mysql", Config.mysqlurl)
	dbtype, dburl := Config.selectdbsystem()
	db, err := sql.Open(dbtype, dburl)
	if err != nil {
		panic(err.Error())
	}
	err = db.Ping()
	if err != nil {
		panic(err.Error())
	}
	return db
}

func sendToMysql(res *Response) error {
	var statement string
	checknameid := getCheckName()
	db := connectToDB()
	defer db.Close()
	for _, hour := range res.Summary.Hours {
		starttime := hour.Starttime.Time.UTC().Format("2006-01-02 15:04:05")
		avgresponcetime := hour.Avgresponse
		downtime := hour.Downtime
		switch dbtype, _ := Config.selectdbsystem(); dbtype {
		case "mysql":
			statement = fmt.Sprintf("INSERT INTO summary_performances (timestamp,%s_avgresponse,%s_downtime) VALUES('%s',%d,%d) ON DUPLICATE KEY UPDATE %s_avgresponse=%d,%s_downtime=%d", checknameid, checknameid, starttime, avgresponcetime, downtime, checknameid, avgresponcetime, checknameid, downtime)
		case "postgres":
			statement = fmt.Sprintf("INSERT INTO %s.summary_performances (timestamp,%s_avgresponse,%s_downtime) VALUES('%s',%d,%d) ON CONFLICT (timestamp) DO UPDATE SET %s_avgresponse=%d, %s_downtime=%d", Config.pgschema, checknameid, checknameid, starttime, avgresponcetime, downtime, checknameid, avgresponcetime, checknameid, downtime)
		}
		// fmt.Println(statement)
		_, err := db.Exec(statement)
		if err != nil {
			panic(err.Error())
		}
	}
	return nil
}
func initializeTable() {
	db := connectToDB()
	defer db.Close()
	var err error
	var statement string
	switch dbtype, _ := Config.selectdbsystem(); dbtype {
	case "mysql":
		statement = `CREATE TABLE IF NOT EXISTS summary_performances (timestamp DATETIME PRIMARY KEY);`
	case "postgres":
		statement = `CREATE TABLE IF NOT EXISTS summary_performances (timestamp timestamp PRIMARY KEY);`
	}
	_, err = db.Exec(statement)
	if err != nil {
		panic(err.Error())
	}
}
func addCheckID(checkid string) {
	db := connectToDB()
	defer db.Close()
	// TODO: Add check for if the column exists
	checknameid := getCheckName()
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s.summary_performances ADD COLUMN %s_avgresponse int, ADD COLUMN %s_downtime int", Config.pgschema, checknameid, checknameid))
	if err != nil {
  log.Panicln("Could not ALTER TABLE to create new check (maybe check exists? Error is:", err)
	}
}
func main() {
	if Config.inittable {
		initializeTable()
		os.Exit(0)
	}
	if Config.addcheck {
		addCheckID(Config.checkid)
		os.Exit(0)
	}
	res, err := getPingdomData()
	if err != nil {
		log.Panicln("Something went wrong requesting the json in the API:", err)
	}
	if Config.output == "console" {
		consoleOutput(res)
	} else if Config.output == "db" {
		sendToMysql(res)
	}
}
