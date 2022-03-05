package schema

type Transaction struct {
	Time            int64
	ChainHash       string `gorm:"uniqueIndex,not null"`
	ChainID         string `gorm:"uniqueIndex:idx_chain_id_addrs;uniqueIndex:idx_chain_id_contract_to_addr;index:idx_chain_id_contract_addr2;index:idx_chain_id_to_addr;index:idx_chain_id_from_addr"`
	Status          uint64 `gorm:"type:numeric"`
	BlockNumber     int64
	Hash            string `gorm:"uniqueIndex:idx_chain_id_addrs;uniqueIndex:idx_chain_id_contract_to_addr"`
	ToAddr          string `gorm:"uniqueIndex:idx_chain_id_addrs;index:idx_chain_id_contract_addr2;index:idx_chain_id_to_addr"`
	FromAddr        string `gorm:"uniqueIndex:idx_chain_id_addrs;index:idx_chain_id_from_addr"`
	Value           int64
	Gas             int64
	GasPrice        int64
	ContractToAddr  string `gorm:"uniqueIndex:idx_chain_id_addrs;uniqueIndex:idx_chain_id_contract_to_addr;index:idx_chain_id_contract_addr2"`
	ContractValue   uint64 `gorm:"type:numeric"`
	ContractTokenID int64
	Data            []byte `json:"-" gorm:"-"`
	// added by us
	Level int
}

type Provenance struct {
	RootAddress         string
	Address             string
	Network             string
	Level               int
	MaxDegrees          int
	Value               int64
	AssociatedAddresses []string
	Children            []*Provenance
	// for output
	ParentAddress string
}
