package ripple

type BlockchainStore struct {
	backend   string
	path      string
	dictStore map[string][]byte
}

func NewBlockchainStore(backend, path string) *BlockchainStore {
	return &BlockchainStore{
		backend:   backend,
		path:      path,
		dictStore: make(map[string][]byte),
	}
}

func (s *BlockchainStore) Get(key string) []byte {
	if s == nil {
		return nil
	}
	if v, ok := s.dictStore[key]; ok {
		return v
	}
	return nil
}

func (s *BlockchainStore) Put(key string, value []byte) bool {
	if s == nil {
		return false
	}
	s.dictStore[key] = value
	return true
}

func (s *BlockchainStore) Delete(key string) bool {
	if s == nil {
		return false
	}
	if _, ok := s.dictStore[key]; !ok {
		return false
	}
	delete(s.dictStore, key)
	return true
}

