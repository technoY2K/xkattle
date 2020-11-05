package database

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// State is the main business logic of the ledger
type State struct {
	Balances  map[Account]uint
	txMempool []Tx

	dbFile          *os.File
	latestBlockHash Hash
}

// NewStateFromDisk starts the ledger from the genesis
func NewStateFromDisk() (*State, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	gen, err := loadGenesis(filepath.Join(cwd, "database", "genesis.json"))
	if err != nil {
		return nil, err
	}

	balances := make(map[Account]uint)
	for account, balance := range gen.Balances {
		balances[account] = balance
	}

	f, err := os.OpenFile(filepath.Join(cwd, "database", "block.db"), os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(f)

	state := &State{balances, make([]Tx, 0), f, Hash{}}

	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}

		dbFSJSON := scanner.Bytes()
		var dbFS dbFS
		err = json.Unmarshal(dbFSJSON, &dbFS)
		if err != nil {
			return nil, err
		}

		err = state.applyBlock(dbFS.Value)
		if err != nil {
			return nil, err
		}

		state.latestBlockHash = dbFS.Key
	}

	err = state.doSnapshot()
	if err != nil {
		return nil, err
	}

	return state, nil
}

// LatestSnapshot returns the current hash of the tx.db file
func (s *State) LatestSnapshot() Snapshot {
	return s.snapshot
}

// Add a tx to the state mempool
func (s *State) Add(tx Tx) error {
	if err := s.apply(tx); err != nil {
		return err
	}
	s.txMempool = append(s.txMempool, tx)
	return nil
}

// Persist mempool to disk
func (s *State) Persist() (Snapshot, error) {
	mempool := make([]Tx, len(s.txMempool))
	copy(mempool, s.txMempool)

	for i := 0; i < len(mempool); i++ {
		txJSON, err := json.Marshal(s.txMempool[i])
		if err != nil {
			return Snapshot{}, err
		}

		fmt.Printf("Persisting new TX to disk:\n")
		fmt.Printf("\t%s\n", txJSON)
		if _, err = s.dbFile.Write(append(txJSON, '\n')); err != nil {
			return Snapshot{}, err
		}

		err = s.doSnapshot()
		if err != nil {
			return Snapshot{}, err
		}
		fmt.Printf("New DB Snapshot: %x\n", s.snapshot)

		s.txMempool = append(s.txMempool[:i], s.txMempool[i+1:]...)
	}
	return s.snapshot, nil
}

// Close references to the file
func (s *State) Close() {
	s.dbFile.Close()
}

func (s *State) apply(tx Tx) error {
	if tx.IsReward() {
		s.Balances[tx.To] += tx.Value
		return nil
	}

	if s.Balances[tx.From] < tx.Value {
		return fmt.Errorf("insufficient balance")
	}

	s.Balances[tx.From] -= tx.Value
	s.Balances[tx.To] += tx.Value

	return nil
}

func (s *State) doSnapshot() error {
	_, err := s.dbFile.Seek(0, 0)
	if err != nil {
		return err
	}

	txsData, err := ioutil.ReadAll(s.dbFile)
	if err != nil {
		return err
	}
	s.snapshot = sha256.Sum256(txsData)

	return nil
}
