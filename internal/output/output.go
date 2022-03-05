package output

import (
	"encoding/csv"
	"encoding/json"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/robertlestak/humun-provenance/internal/schema"
	log "github.com/sirupsen/logrus"
)

func csvFileToFlatArr(fileName string) ([]*schema.Provenance, error) {
	l := log.WithFields(log.Fields{
		"package": "output",
		"func":    "csvFileToFlatArr",
		"file":    fileName,
	})
	l.Info("Converting csv file to flat array")
	var flatArr []*schema.Provenance
	f, err := os.Open(fileName)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	csvReader := csv.NewReader(f)
	data, err := csvReader.ReadAll()
	if err != nil {
		l.Error(err)
		return nil, err
	}
	for i, row := range data {
		if i == 0 {
			continue
		}
		// rootAddress
		// address
		// network
		// level
		// value
		// parentAddress

		// convert row[4] to int64
		intVal, err := strconv.ParseInt(row[4], 10, 64)
		if err != nil {
			l.Error(err)
			return nil, err
		}
		intLevel, err := strconv.Atoi(row[3])
		if err != nil {
			l.Error(err)
			return nil, err
		}
		flatArr = append(flatArr, &schema.Provenance{
			RootAddress:   row[0],
			Address:       row[1],
			Network:       row[2],
			Level:         intLevel,
			Value:         intVal,
			ParentAddress: row[5],
		})
	}
	return flatArr, nil
}

func makeRecursiveArr(provs []*schema.Provenance) []*schema.Provenance {
	l := log.WithFields(log.Fields{
		"package": "output",
		"func":    "makeRecursiveArr",
		"provLen": len(provs),
	})
	l.Info("Making recursive array")
	var recursiveArr []*schema.Provenance
	var rootAddr string
	parentBlocks := make(map[string][]*schema.Provenance)

	for _, prov := range provs {
		// grab the root address
		if rootAddr == "" {
			rootAddr = prov.RootAddress
		}
		// set all the parent / child relationships in a flat map
		parentBlocks[prov.ParentAddress] = append(parentBlocks[prov.ParentAddress], prov)
	}

	for _, prov := range provs {
		if prov.ParentAddress == rootAddr {
			recursiveArr = append(recursiveArr, prov)
		}
	}

	l.Info("Parent blocks: ", parentBlocks)
	return recursiveArr
}

func csvFileToJSON(fileName string) ([]byte, error) {
	l := log.WithFields(log.Fields{
		"package": "output",
		"func":    "csvFileToJSON",
		"file":    fileName,
	})
	l.Info("Converting csv file to json")
	// read csv file into flat struct array
	// build relations
	provs, err := csvFileToFlatArr(fileName)
	if err != nil {
		l.Error(err)
		return nil, err
	}
	rec := makeRecursiveArr(provs)

	// convert to json
	jd, jerr := json.Marshal(rec)
	if jerr != nil {
		l.Error(jerr)
		return nil, jerr
	}
	return jd, nil
}

func CsvFileToJSONFile(in string, out string) error {
	l := log.WithFields(log.Fields{
		"package": "output",
		"func":    "CsvFileToJSONFile",
		"in":      in,
		"out":     out,
	})
	l.Info("Converting csv file to json file")
	jd, err := csvFileToJSON(in)
	if err != nil {
		l.Error(err)
		return err
	}
	// write to out file
	if werr := ioutil.WriteFile(out, jd, 0644); werr != nil {
		l.Error(werr)
		return werr
	}
	return nil
}
