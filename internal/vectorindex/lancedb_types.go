package vectorindex

// LanceDBConfig holds configuration for LanceDBIndex.
type LanceDBConfig struct {
	// Dir is the directory where LanceDB stores its data files.
	Dir string

	// Dims is the dimensionality of the embedding vectors.
	Dims int
}
