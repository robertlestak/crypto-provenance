package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/robertlestak/humun-provenance/internal/cache"
	"github.com/robertlestak/humun-provenance/internal/schema"
	"github.com/rs/cors"
	log "github.com/sirupsen/logrus"
)

var (
	wg sync.WaitGroup
	ml sync.Mutex

	touchedAddrs []string

	pendingQueue []schema.Provenance
)

type OutputBuilderResponse struct {
	Output []*schema.Provenance
	Error  error
}

func init() {
	ll, err := log.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		ll = log.InfoLevel
	}
	log.SetLevel(ll)
	if err := cache.Init(); err != nil {
		log.Fatal(err)
	}
}

func txGetter(p schema.Provenance) error {
	l := log.WithFields(log.Fields{
		"func": "txGetter",
	})
	l.Info("start")
	var hasMore bool = true
	var page int = 1
	var maxPage int
	var err error
	if os.Getenv("MAX_TX_PAGES") != "" {
		maxPage, err = strconv.Atoi(os.Getenv("MAX_TX_PAGES"))
		if err != nil {
			l.Error(err)
			return err
		}
	}
	for hasMore {
		// TODO: this needs to be done in a loop until we get all txs
		txs, err := getLatestTxs(p.Network, p.Address, page, 1000)
		if err != nil {
			log.Error(err)
			return err
		}
		if len(txs) == 0 {
			hasMore = false
			break
		}
		var nt []schema.Transaction
		if p.MaxDegrees > 0 && p.Level > p.MaxDegrees+1 {
			l.Info("max degrees reached")
			return nil
		} else {
			l.Debugf("max degrees: %d, level: %d", p.MaxDegrees, p.Level)
		}
		for _, tx := range txs {
			lt := tx
			lt.Level = p.Level + 1
			nt = append(nt, lt)
		}

		if err := cache.AddTxs(p.RootAddress, p.Address, nt); err != nil {
			log.Error(err)
			return err
		}
		page++
		if maxPage > 0 && page > maxPage {
			hasMore = false
		}
	}
	return nil
}

func txAddrGetterWorker(ps chan schema.Provenance, res chan error) {
	l := log.WithFields(log.Fields{
		"func": "txAddrGetterWorker",
	})
	l.Info("start")
	for p := range ps {
		l.Infof("addr: %s", p.Address)
		if err := txAddrGetter(p); err != nil {
			l.Error(err)
			res <- err
		}
	}
	res <- nil
}

func txAddrGetter(p schema.Provenance) error {
	l := log.WithFields(log.Fields{
		"func": "txAddrGetter",
	})
	l.Info("start")
	moreTxs := true
	for moreTxs {
		tx, err := cache.GetNextTx(p.RootAddress, p.Address)
		if err != nil && err != cache.ErrNoMoreTxs {
			l.Error(err)
			return err
		} else if err == cache.ErrNoMoreTxs {
			l.Info("no more txs")
			moreTxs = false
			break
		}
		l.Debugf("tx: %+v", tx)
		// check if address is already in the list
		isMem, err := cache.CheckAssociatedAddress(p.Address, tx.FromAddr)
		if err != nil {
			log.Error(err)
			return err
		}
		// this is a new address
		if !isMem {
			// add the address to the list
			err = cache.AddAssociatedAddress(p.Address, tx.FromAddr)
			if err != nil {
				log.Error(err)
				return err
			}
			// get the txs for the new address
			np := schema.Provenance{
				Address: tx.FromAddr,
				Network: tx.ChainID,
				// should we use the root level or new for each
				Level:       tx.Level,
				RootAddress: p.RootAddress,
				MaxDegrees:  p.MaxDegrees,
			}
			if err := txGetter(np); err != nil && err != cache.ErrNoMoreTxs {
				log.Error(err)
				return err
			}
			l.Infof("new addr: %s", tx.FromAddr)
		} else {
			l.Debugf("tx from addr %s already in associated addresses", tx.FromAddr)
		}
		if tx.FromAddr == "" {
			l.Error("tx from addr is empty")
			continue
		}
		if err := cache.SetStepAddressValue(tx.ToAddr, tx.Level, tx.FromAddr, tx.Value); err != nil {
			log.Error(err)
			return err
		}
	}
	return nil
}

func txAddrGetterProcess(p schema.Provenance) error {
	l := log.WithFields(log.Fields{
		"func": "txAddrGetterProcess",
	})
	l.Info("start")
	workerCount := 5
	ps := make(chan schema.Provenance, workerCount)
	res := make(chan error, workerCount)
	for i := 0; i < workerCount; i++ {
		go txAddrGetterWorker(ps, res)
	}
	ps <- p
	close(ps)
	for i := 0; i < workerCount; i++ {
		if err := <-res; err != nil {
			return err
		}
	}
	wg.Done()
	return nil
}

func txBackfiller(p schema.Provenance) error {
	l := log.WithFields(log.Fields{
		"func": "txBackfiller",
	})
	l.Info("start")
	hasMore := true
	var emptycount int
	var addrs []string
	for hasMore {
		var err error
		if len(addrs) == 0 {
			addrs, err = cache.GetPendingAddrsForRoot(p.RootAddress)
			if err != nil {
				l.Error(err)
				return err
			}
		}
		// we either have no addresses or there is an issue with the scan,
		// wait a bit and then try a keys
		if len(addrs) == 0 {
			l.Info("no more addrs")
			if emptycount > 3 {
				l.Info("no more addrs and emptycount > 10")
				la, err := cache.GetPendingAddrsForRootKeys(p.RootAddress)
				if err != nil {
					l.Error(err)
					return err
				}
				if len(la) == 0 {
					l.Info("no more addrs and emptycount > 10 and no more keys")
					hasMore = false
				}
				addrs = la
				emptycount = 0
			}
			time.Sleep(time.Second)
			emptycount++
			continue
		}
		for _, addr := range addrs {
			l.Infof("addr: %s", addr)
			np := schema.Provenance{
				Address:     addr,
				Network:     p.Network,
				Level:       p.Level,
				MaxDegrees:  p.MaxDegrees,
				RootAddress: p.RootAddress,
			}
			wg.Add(1)
			if err := txAddrGetterProcess(np); err != nil {
				log.Error(err)
				return err
			}
		}
		// kinda really hacky
		addrs = []string{}
	}
	wg.Done()
	return nil
}

func csvRowHeader() string {
	var row []string
	row = append(row, "rootAddress")
	row = append(row, "address")
	row = append(row, "network")
	row = append(row, "level")
	//row = append(row, "maxDegrees")
	row = append(row, "value")
	row = append(row, "parentAddress")
	return strings.Join(row, ",")
}

func provenanceToCSV(p schema.Provenance) []string {
	var row []string
	row = append(row, p.RootAddress)
	row = append(row, p.Address)
	row = append(row, p.Network)
	row = append(row, strconv.Itoa(p.Level))
	//row = append(row, strconv.Itoa(p.MaxDegrees))
	row = append(row, strconv.FormatInt(p.Value, 10))
	row = append(row, p.ParentAddress)
	return row
}

func writeHeaderToDisk(fileName string) error {
	l := log.WithFields(log.Fields{
		"func": "writeHeaderToDisk",
	})
	l.Info("start")
	// only write header if file doesn't exist
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		f, err := os.Create(fileName)
		if err != nil {
			l.Error(err)
			return err
		}
		defer f.Close()
		_, err = f.WriteString(csvRowHeader() + "\n")
		if err != nil {
			l.Error(err)
			return err
		}
	}
	return nil
}

func writeOutputToDisk(p schema.Provenance, fileName string) error {
	l := log.WithFields(log.Fields{
		"func": "writeOutputToDisk",
	})
	l.Debug("start")
	var err error
	csvDataStr := strings.Join(provenanceToCSV(p), ",") + "\n"
	csvData := []byte(csvDataStr)
	ml.Lock()
	// append csvData to file
	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		l.Error(err)
		return err
	}
	defer f.Close()
	if _, err = f.Write(csvData); err != nil {
		l.Error(err)
		return err
	}
	ml.Unlock()
	return err
}

// given root address, get all child addresses
// for each child address, get all child addresses
func outputBuilder(p schema.Provenance, outFile string) error {
	l := log.WithFields(log.Fields{
		"func": "outputBuilder",
	})
	l.Info("start")
	stepKeys, err := cache.GetStepAddressKeys(p.Address)
	if err == cache.ErrNoMoreTxs {
		l.Info("no step keys")
		return nil
	} else if err != nil {
		l.Error(err)
		return err
	}
StepKeys:
	for _, stepKey := range stepKeys {
		spl := strings.Split(stepKey, ":")
		if len(spl) != 2 {
			l.Errorf("invalid stepKey: %s", stepKey)
			continue
		}
		level, err := strconv.Atoi(spl[0])
		if err != nil {
			l.Error(err)
			return err
		}
		addr := spl[1]
		l.Infof("level: %d, addr: %s", level, addr)
		stepAddrVal, err := cache.GetStepAddressValue(p.Address, addr, level)
		if err == cache.ErrNoMoreTxs {
			l.Info("no step keys")
			continue
		} else if err != nil {
			l.Error(err)
			return err
		}
		l.Debugf("stepAddrVal: %+v", stepAddrVal)
		//l.Debugf("stepAddrVal: %+v", stepAddrVal)
		lp := &schema.Provenance{
			Address:       addr,
			Network:       p.Network,
			Level:         level,
			Value:         stepAddrVal,
			ParentAddress: p.Address,
			MaxDegrees:    p.MaxDegrees,
			RootAddress:   p.RootAddress,
		}
		l.Debugf("lp: %+v", lp)
		// write to disk
		if err := writeOutputToDisk(*lp, outFile); err != nil {
			l.Error(err)
			return err
		}
		for _, t := range touchedAddrs {
			if t == addr {
				l.Debugf("addr: %s already touched", addr)
				continue StepKeys
			}
		}
		touchedAddrs = append(touchedAddrs, addr)
		if err := outputBuilder(*lp, outFile); err != nil {
			l.Error(err)
			return err
		}
	}
	return nil
}

func cleanupCache(p schema.Provenance) error {
	l := log.WithFields(log.Fields{
		"func": "cleanupCache",
	})
	l.Info("start")
	for _, t := range touchedAddrs {
		if err := cache.DelStepAddress(t); err != nil {
			l.Error(err)
			return err
		}
		if err := cache.DelAddr(t); err != nil {
			l.Error(err)
			return err
		}
	}
	if err := cache.DelStepAddress(p.RootAddress); err != nil {
		l.Error(err)
		return err
	}
	if err := cache.DelAddr(p.RootAddress); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func outputData(p schema.Provenance, filename string) error {
	l := log.WithFields(log.Fields{
		"func": "outputData",
	})
	l.Info("start")
	os.Remove(filename)
	if err := writeHeaderToDisk(filename); err != nil {
		l.Error(err)
		return err
	}
	err := outputBuilder(p, filename)
	if err != nil {
		l.Error(err)
		return err
	}
	/*
		l.Debugf("output: %+v", output)
		jd, err := json.Marshal(output)
		if err != nil {
			l.Error(err)
			return err
		}
		err = ioutil.WriteFile(filename, jd, 0644)
		if err != nil {
			l.Error(err)
			return err
		}
		removeBuildingOutput(p.RootAddress)
	*/
	if err := cleanupCache(p); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func txworker(p schema.Provenance, level int) error {
	l := log.WithFields(log.Fields{
		"func": "txworker",
	})
	l.Info("start")
	outputDir := os.Getenv("OUTPUT_DIR")
	outFile := fmt.Sprintf("%s/%s_%s.csv", outputDir, p.Network, p.RootAddress)
	if err := txGetter(p); err != nil {
		l.Error(err)
	}
	wg.Add(2)
	go txAddrGetterProcess(p)
	go txBackfiller(p)
	wg.Wait()
	if err := outputData(p, outFile); err != nil {
		l.Error(err)
		return err
	}
	/*
		// optional, output json file
		jsonOutFile := fmt.Sprintf("%s/%s.json", outputDir, p.RootAddress)
		if err := output.CsvFileToJSONFile(outFile, jsonOutFile); err != nil {
			l.Error(err)
			return err
		}
	*/
	return nil
}

func process(p schema.Provenance) {
	l := log.WithFields(log.Fields{
		"func": "process",
	})
	l.Info("start")
	if err := txworker(p, 0); err != nil {
		l.Error(err)
	}
}

func handleNewProvenance(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{
		"func": "handleNewProvenance",
	})
	l.Info("start")
	var network string = r.FormValue("network")
	var rootAddress string = r.FormValue("rootAddress")
	var maxDegrees int
	maxDegStr := r.FormValue("maxDegrees")
	if maxDegStr != "" {
		var err error
		maxDegrees, err = strconv.Atoi(maxDegStr)
		if err != nil {
			l.Error(err)
			return
		}
	} else {
		maxDegrees = 5
	}
	var res struct {
		Status      string `json:"status,omitempty"`
		FileName    string `json:"fileName,omitempty"`
		QueueLength int    `json:"queueLength,omitempty"`
		Error       string `json:"error,omitempty"`
	}
	if network == "" {
		l.Error("network is required")
		res.Status = "error"
		res.Error = "network is required"
		json.NewEncoder(w).Encode(res)
		return
	}
	if rootAddress == "" {
		l.Error("rootAddress is required")
		res.Status = "error"
		res.Error = "rootAddress is required"
		json.NewEncoder(w).Encode(res)
		return
	}
	p := schema.Provenance{
		Network:     network,
		RootAddress: rootAddress,
		MaxDegrees:  maxDegrees,
		Address:     rootAddress,
	}
	pendingQueue = append(pendingQueue, p)
	fn := fmt.Sprintf("%s_%s.csv", network, rootAddress)
	res.Status = "pending"
	res.FileName = fn
	res.QueueLength = len(pendingQueue)
	jd, err := json.Marshal(res)
	if err != nil {
		l.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(jd)
	//go process(p)
}

func serverQueueWorker() {
	l := log.WithFields(log.Fields{
		"func": "serverQueueWorker",
	})
	l.Info("start")
	for {
		time.Sleep(time.Second)
		l.Debug("check pending queue")
		if len(pendingQueue) > 0 {
			l.Debugf("pending queue length: %d", len(pendingQueue))
			p := pendingQueue[0]
			// remove from queue
			l.Debugf("remove from queue: %+v", p)
			pendingQueue = pendingQueue[1:]
			l.Debugf("process pending provenance: %+v", p)
			process(p)
		}
	}
}

func server() error {
	l := log.WithFields(log.Fields{
		"func": "server",
	})
	l.Info("start")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go serverQueueWorker()
	r := mux.NewRouter()
	r.HandleFunc("/", handleNewProvenance).Methods("GET")
	l.Infof("Listening on port %s", port)
	c := cors.New(cors.Options{
		AllowedOrigins:   strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ","),
		AllowedHeaders:   strings.Split(os.Getenv("CORS_ALLOWED_HEADERS"), ","),
		AllowedMethods:   strings.Split(os.Getenv("CORS_ALLOWED_METHODS"), ","),
		AllowCredentials: true,
		Debug:            os.Getenv("CORS_DEBUG") == "true",
	})
	h := c.Handler(r)
	if err := http.ListenAndServe(":"+port, h); err != nil {
		return err
	}
	return nil
}

func cli() {
	l := log.WithFields(log.Fields{
		"func": "cli",
	})
	l.Info("start")
	// parse flags
	fs := flag.NewFlagSet("cli", flag.ExitOnError)
	var network string
	var rootAddress string
	var maxDegrees int
	fs.StringVar(&network, "n", "", "network")
	fs.StringVar(&rootAddress, "a", "", "rootAddress")
	fs.IntVar(&maxDegrees, "d", 0, "maxDegrees")
	fs.Parse(os.Args[2:])
	if network == "" {
		l.Error("network is required")
		os.Exit(1)
	}
	if rootAddress == "" {
		l.Error("rootAddress is required")
		os.Exit(1)
	}
	p := schema.Provenance{
		Network:     network,
		RootAddress: rootAddress,
		MaxDegrees:  maxDegrees,
		Address:     rootAddress,
	}
	process(p)
}

func main() {
	l := log.WithFields(log.Fields{
		"func": "main",
	})
	l.Info("start")
	if len(os.Args) < 2 {
		l.Error("no command")
		return
	}
	switch os.Args[1] {
	case "server":
		if err := server(); err != nil {
			l.Error(err)
		}
	case "cli":
		cli()
	default:
		l.Error("unknown command")
	}
}

// given an address, get all the received transactions
// for each transaction, get the sender address, date, and amount
// for each sender address:
//   check if address already exists, if not
//	 get all received transactions, in loop
