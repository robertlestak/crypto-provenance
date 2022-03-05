package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/robertlestak/humun-provenance/internal/schema"
	log "github.com/sirupsen/logrus"
)

var (
	Client *redis.Client
)

const (
	txprefix     = "txs"
	addrsprefix  = "addrs"
	stepprefix   = "steps"
	ErrNoMoreTxs = redis.Nil
)

func Init() error {
	l := log.WithFields(log.Fields{
		"package": "cache",
	})
	l.Info("Initializing redis client")
	Client = redis.NewClient(&redis.Options{
		Addr:        fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")),
		Password:    "", // no password set
		DB:          0,  // use default DB
		DialTimeout: 30 * time.Second,
		ReadTimeout: 30 * time.Second,
	})
	cmd := Client.Ping()
	if cmd.Err() != nil {
		l.Error("Failed to connect to redis")
		return cmd.Err()
	}
	l.Info("Connected to redis")
	return nil
}

func AddTxs(rootAddress string, address string, txs []schema.Transaction) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "AddTxs",
		"root":    rootAddress,
		"address": address,
		"txs":     len(txs),
	})
	l.Info("Adding txs")
	key := fmt.Sprintf("%s:%s:%s", txprefix, rootAddress, address)
	for _, t := range txs {
		l.Debugf("Adding tx %s", key)
		jd, jerr := json.Marshal(t)
		if jerr != nil {
			l.Error(jerr)
			return jerr
		}
		if err := Client.LPush(key, jd).Err(); err != nil {
			l.Error(err)
			return err
		}
	}
	return nil
}

func AddAssociatedAddress(rootAddress string, associatedAddress string) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "AddAssociatedAddress",
		"root":    rootAddress,
		"address": associatedAddress,
	})
	l.Info("Adding associated address")
	key := fmt.Sprintf("%s:%s", addrsprefix, rootAddress)
	if err := Client.SAdd(key, associatedAddress).Err(); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func CheckAssociatedAddress(rootAddress string, address string) (bool, error) {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "CheckAssociatedAddress",
		"root":    rootAddress,
		"address": address,
	})
	l.Info("Checking associated address")
	key := fmt.Sprintf("%s:%s", addrsprefix, rootAddress)
	return Client.SIsMember(key, address).Result()
}

func GetNextTx(rootAddress string, address string) (schema.Transaction, error) {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "GetNextTx",
		"root":    rootAddress,
		"address": address,
	})
	l.Info("Getting next tx")
	key := fmt.Sprintf("%s:%s:%s", txprefix, rootAddress, address)
	jd, err := Client.RPop(key).Bytes()
	if err != nil {
		l.Error(err)
		return schema.Transaction{}, err
	}
	var tx schema.Transaction
	if err := json.Unmarshal(jd, &tx); err != nil {
		l.Error(err)
		return schema.Transaction{}, err
	}
	return tx, nil
}

func GetPendingAddrsForRoot(rootAddress string) ([]string, error) {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "GetPendingAddrsForRoot",
		"root":    rootAddress,
	})
	l.Info("Getting pending tx addrs for root")
	key := fmt.Sprintf("%s:%s:*", txprefix, rootAddress)
	l.Debugf("Getting keys for %s", key)
	var keys []string
	var addrs []string
	var cursor uint64
	var initCursor bool = true
	for cursor > 0 || initCursor {
		res := Client.Scan(cursor, key, 1000)
		if res.Err() != nil {
			l.Error(res.Err())
			return nil, res.Err()
		}
		keys, cursor = res.Val()
		if len(keys) > 0 {
			l.Debug("Found outstanding addrs")
		} else {
			l.Debug("No outstanding addrs")
			break
		}
		for _, k := range keys {
			spl := strings.Split(k, ":")
			addrs = append(addrs, spl[2])
		}
		l.Debugf("Found %d outstanding addrs", len(addrs))
		initCursor = false
	}
	return addrs, nil
}

func GetPendingAddrsForRootKeys(rootAddress string) ([]string, error) {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "GetPendingAddrsForRootKeys",
		"root":    rootAddress,
	})
	l.Info("Getting pending tx addrs for root")
	key := fmt.Sprintf("%s:%s:*", txprefix, rootAddress)
	l.Debugf("Getting keys for %s", key)
	var keys []string
	var addrs []string
	var cursor uint64
	var initCursor bool = true
	for cursor > 0 || initCursor {
		res := Client.Keys(key)
		if res.Err() != nil {
			l.Error(res.Err())
			return nil, res.Err()
		}
		keys = res.Val()
		if len(keys) > 0 {
			l.Debug("Found outstanding addrs")
		} else {
			l.Debug("No outstanding addrs")
			break
		}
		for _, k := range keys {
			spl := strings.Split(k, ":")
			addrs = append(addrs, spl[2])
		}
		l.Debugf("Found %d outstanding addrs", len(addrs))
		initCursor = false
	}
	return addrs, nil
}

func SetStepAddressValue(rootAddress string, step int, address string, value int64) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "SetStepAddressValue",
		"root":    rootAddress,
		"step":    step,
		"address": address,
		"value":   value,
	})
	l.Info("Setting step address value")
	key := fmt.Sprintf("%s:%s", stepprefix, rootAddress)
	if err := Client.HSet(key, fmt.Sprintf("%d:%s", step, address), value).Err(); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func GetStepAddressKeys(address string) ([]string, error) {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "GetStepAddress",
		"address": address,
	})
	var keys []string
	l.Info("Getting step address")
	key := fmt.Sprintf("%s:%s", stepprefix, address)
	res := Client.HKeys(key)
	if res.Err() != nil {
		l.Error(res.Err())
		return keys, res.Err()
	}
	keys = res.Val()
	return keys, nil
}

func GetStepAddressValue(parentAddress string, address string, step int) (int64, error) {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "GetStepAddressValue",
		"parent":  parentAddress,
		"address": address,
		"step":    step,
	})
	l.Info("Getting step address value")
	key := fmt.Sprintf("%s:%s", stepprefix, parentAddress)
	res := Client.HGet(key, fmt.Sprintf("%d:%s", step, address))
	if res.Err() != nil {
		l.Error(res.Err())
		return 0, res.Err()
	}
	val, err := res.Int64()
	if err != nil {
		l.Error(err)
		return 0, err
	}
	return val, nil
}

func DelStepAddressStep(address string, step int) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "DelStepAddressStep",
		"address": address,
		"step":    step,
	})
	l.Info("Deleting step address")
	key := fmt.Sprintf("%s:%s", stepprefix, address)
	if err := Client.HDel(key, fmt.Sprintf("%d", step)).Err(); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func DelStepAddress(address string) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "DelStepAddress",
		"address": address,
	})
	l.Info("Deleting step address")
	key := fmt.Sprintf("%s:%s", stepprefix, address)
	if err := Client.Del(key).Err(); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func DelAddr(address string) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"func":    "DelAddr",
		"address": address,
	})
	l.Info("Deleting address")
	key := fmt.Sprintf("%s:%s", addrsprefix, address)
	if err := Client.Del(key).Err(); err != nil {
		l.Error(err)
		return err
	}
	return nil
}
