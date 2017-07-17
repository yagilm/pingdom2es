package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	flag "github.com/ogier/pflag"
)

// Timestamp is defined here as per
// https://gist.github.com/bsphere/8369aca6dde3e7b4392c#gistcomment-1413740
type Timestamp struct {
	time.Time
}

// MarshalJSON time2json
func (t *Timestamp) MarshalJSON() ([]byte, error) {
	ts := t.Time.Unix()
	stamp := fmt.Sprint(ts)
	return []byte(stamp), nil
}

// UnmarshalJSON json2time
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	ts, err := strconv.Atoi(string(b))
	if err != nil {
		return err
	}
	t.Time = time.Unix(int64(ts), 0)
	return nil
}

// Lasttimeserie is the global store to remember which is the last timeserie we
// sent to statsd. When not assigned, it is '0001-01-01 00:00:00 +0000 UTC',
// thus earlier of any possible timeserie
// --will not need if ES?
// var Lasttimeserie time.Time

// Configuration options
type Configuration struct {
	usermail      string
	pass          string
	headerXappkey string
	checkname     string // name of the check, ex summary.average
	checkid       string // id of the check, aka, which domain are we checking
	from          int32
	to            int32
}

// Config keeps the configuration
var Config Configuration
var version = "development"

// Check if configuration is invalid
func (conf Configuration) configurationInvalid() bool {
	return conf.usermail == "" ||
		conf.pass == "" ||
		conf.headerXappkey == "" ||
		conf.checkname == "" ||
		conf.checkid == ""
}

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
	flag.StringVar(&Config.usermail, "email", "", "e-mail account for pingdom's API")
	flag.StringVar(&Config.pass, "pass", "", "password for pingdom's API")
	flag.StringVar(&Config.headerXappkey, "appkey", "", "Appkey for pingdom's API")
	flag.StringVar(&Config.checkname, "checkname", "", "Name of the check (eg summary.average)") //multiple checks seperated by comma?
	flag.StringVar(&Config.checkid, "checkid", "", "id of the check, which domain are we checking?")
	flag.Int32Var(&Config.from, "from", int32(time.Now().Add(-24*time.Hour).Unix()), "from which (Unix)time we are asking, default 24 hours ago =>")
	flag.Int32Var(&Config.to, "to", int32(time.Now().Unix()), "until which (Unix)time we are asking, default: now =>")
	flag.Usage = func() {
		fmt.Printf("Version: %s\nUsage: pingdom2mysql [options]\nRequired options:\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()
	if Config.configurationInvalid() {
		flag.Usage()
		os.Exit(1)
	}
}

// Gets data from Pingdom's API
func getpingdomdata() (*Response, error) {
	// make the request with the appropriate headers
	req, err := http.NewRequest("GET",
		fmt.Sprintf(
			"https://api.pingdom.com/api/2.0/%s/%s?from=%d&to=%d&includeuptime=true", //TODO Add: ?from=$(date -d '1 minute ago' +"%s")\&includeuptime=true
			Config.checkname,
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
	// client := &http.Client{}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("API not 200")
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var response Response
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}
	response.Summary.Hours = response.Summary.Hours[:len(response.Summary.Hours)-1]
	// fmt.Println(response.Summary.Hours[0].Starttime.String())
	return &response, err
}

// Send statistics to the data store
// func sendstatistics() {
// I think I will be using olivere/elastic https://github.com/olivere/elastic for this to send it to ES
// Nevertheless, we need to decide on a data store....
// }
func sendtomysql(res *Response) error {

	for _, hour := range res.Summary.Hours {
		fmt.Println(hour.Starttime.Time, hour.Uptime, hour.Avgresponse, hour.Downtime)
	}

	// cupcake9:bi-db:$TABLE:
	// Here is the structure of the table:
	// id | timestamp | avg_uptime | avg_responcetime |
	// check if scheme is correct?
	return nil
}

func main() {
	timer := time.NewTicker(time.Second * 2)
	// infinite loop
	for {
		res, err := getpingdomdata()
		if err != nil {
			fmt.Print("Something went wrong requesting the json in the API:", err)
		}
		toprint, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println(string(toprint))
		<-timer.C
	}
}
