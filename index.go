package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"

	"github.com/robertlestak/humun-provenance/internal/schema"
	log "github.com/sirupsen/logrus"
)

func getLatestTxs(network string, address string, page int, pageSize int) ([]schema.Transaction, error) {
	l := log.WithFields(log.Fields{
		"func": "getLatestTxs",
		"net":  network,
		"addr": address,
		"page": page,
		"size": pageSize,
	})
	l.Info("Getting latest transactions")
	var txs []schema.Transaction
	c := &http.Client{}
	ur := fmt.Sprintf("%s/address/received", os.Getenv("INDEX_API"))
	req, err := http.NewRequest("GET", ur, nil)
	if err != nil {
		l.Error(err)
		return txs, err
	}
	req.Header.Set("Content-Type", "application/json")
	if os.Getenv("INDEX_API_JWT") != "" {
		req.Header.Add("Authorization", "Bearer "+os.Getenv("INDEX_API_JWT"))
	}
	params := req.URL.Query()
	params.Add("chain", network)
	params.Add("address", address)
	params.Add("page", strconv.Itoa(page))
	params.Add("pageSize", strconv.Itoa(pageSize))
	params.Add("order", "desc")
	req.URL.RawQuery = params.Encode()
	l.Debugf("Request: %s", req.URL.String())
	if os.Getenv("LOG_LEVEL") == "debug" {
		rd, derr := httputil.DumpRequest(req, true)
		if derr != nil {
			l.Error(derr)
			return txs, derr
		}
		l.Debugf("Request: %s", string(rd))
	}
	resp, err := c.Do(req)
	if err != nil {
		l.Error(err)
		return txs, err
	}
	defer resp.Body.Close()
	bd, berr := ioutil.ReadAll(resp.Body)
	if berr != nil {
		l.Error(berr)
		return txs, berr
	}
	if resp.StatusCode != http.StatusOK {
		l.Errorf("Error getting transactions: %s", resp.Status)
		l.Debugf("Response: %s", string(bd))
		return txs, err
	}
	l.Debugf("Response: %s", string(bd))
	jerr := json.Unmarshal(bd, &txs)
	if jerr != nil {
		l.Error(jerr)
		return txs, jerr
	}
	return txs, nil
}
