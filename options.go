/*
 * Copyright 2017 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package badger

import (
	"time"

	"github.com/dgraph-io/badger/v2/options"
	"github.com/dgraph-io/badger/v2/table"
	"github.com/dgraph-io/badger/v2/y"
)

// Note: If you add a new option X make sure you also add a WithX method on Options.

// Options are params for creating DB object.
//
// This package provides DefaultOptions which contains options that should
// work for most applications. Consider using that as a starting point before
// customizing it for your own needs.
//
// Each option X is documented on the WithX method.
type Options struct {
	// Required options.

	Dir      string
	ValueDir string

	// Usually modified options.

	SyncWrites          bool
	TableLoadingMode    options.FileLoadingMode
	ValueLogLoadingMode options.FileLoadingMode
	NumVersionsToKeep   int
	ReadOnly            bool
	Truncate            bool
	Logger              Logger
	Compression         options.CompressionType
	EventLogging        bool

	// Fine tuning options.

	MaxTableSize        int64
	LevelSizeMultiplier int
	MaxLevels           int
	ValueThreshold      int
	NumMemtables        int
	// Changing BlockSize across DB runs will not break badger. The block size is
	// read from the block index stored at the end of the table.
	BlockSize          int
	BloomFalsePositive float64
	KeepL0InMemory     bool
	MaxCacheSize       int64

	NumLevelZeroTables      int
	NumLevelZeroTablesStall int

	LevelOneSize       int64
	ValueLogFileSize   int64
	ValueLogMaxEntries uint32

	NumCompactors     int
	CompactL0OnClose  bool
	LogRotatesToFlush int32
	// When set, checksum will be validated for each entry read from the value log file.
	VerifyValueChecksum bool

	// Encryption related options.
	EncryptionKey                 []byte        // encryption key
	EncryptionKeyRotationDuration time.Duration // key rotation duration

	// ChecksumVerificationMode decides when db should verify checksum for SStable blocks.
	ChecksumVerificationMode options.ChecksumVerificationMode

	// Transaction start and commit timestamps are managed by end-user.
	// This is only useful for databases built on top of Badger (like Dgraph).
	// Not recommended for most users.
	managedTxns bool

	// 4. Flags for testing purposes
	// ------------------------------
	maxBatchCount int64 // max entries in batch
	maxBatchSize  int64 // max batch size in bytes
}

// DefaultOptions sets a list of recommended options for good performance.
// Feel free to modify these to suit your needs with the WithX methods.
func DefaultOptions(path string) Options {
	defaultCompression := options.ZSTD
	// Use snappy as default compression algorithm if badger is built without CGO.
	if !y.CgoEnabled {
		defaultCompression = options.Snappy
	}
	return Options{
		Dir:                 path,
		ValueDir:            path,
		LevelOneSize:        256 << 20,
		LevelSizeMultiplier: 10,
		TableLoadingMode:    options.MemoryMap,
		ValueLogLoadingMode: options.MemoryMap,
		// table.MemoryMap to mmap() the tables.
		// table.Nothing to not preload the tables.
		MaxLevels:               7,
		MaxTableSize:            64 << 20,
		NumCompactors:           2, // Compactions can be expensive. Only run 2.
		NumLevelZeroTables:      5,
		NumLevelZeroTablesStall: 10,
		NumMemtables:            5,
		BloomFalsePositive:      0.01,
		BlockSize:               4 * 1024,
		SyncWrites:              true,
		NumVersionsToKeep:       1,
		CompactL0OnClose:        true,
		KeepL0InMemory:          true,
		VerifyValueChecksum:     false,
		Compression:             defaultCompression,
		MaxCacheSize:            1 << 30, // 1 GB
		// Nothing to read/write value log using standard File I/O
		// MemoryMap to mmap() the value log files
		// (2^30 - 1)*2 when mmapping < 2^31 - 1, max int32.
		// -1 so 2*ValueLogFileSize won't overflow on 32-bit systems.
		ValueLogFileSize: 1<<30 - 1,

		ValueLogMaxEntries:            1000000,
		ValueThreshold:                32,
		Truncate:                      false,
		Logger:                        defaultLogger,
		LogRotatesToFlush:             2,
		EventLogging:                  true,
		EncryptionKey:                 []byte{},
		EncryptionKeyRotationDuration: 10 * 24 * time.Hour, // Default 10 days.
	}
}

func buildTableOptions(opt Options) table.Options {
	return table.Options{
		BlockSize:          opt.BlockSize,
		BloomFalsePositive: opt.BloomFalsePositive,
		LoadingMode:        opt.TableLoadingMode,
		ChkMode:            opt.ChecksumVerificationMode,
		Compression:        opt.Compression,
	}
}

const (
	maxValueThreshold = (1 << 20) // 1 MB
)

// LSMOnlyOptions follows from DefaultOptions, but sets a higher ValueThreshold
// so values would be collocated with the LSM tree, with value log largely acting
// as a write-ahead log only. These options would reduce the disk usage of value
// log, and make Badger act more like a typical LSM tree.
func LSMOnlyOptions(path string) Options {
	// Let's not set any other options, because they can cause issues with the
	// size of key-value a user can pass to Badger. For e.g., if we set
	// ValueLogFileSize to 64MB, a user can't pass a value more than that.
	// Setting it to ValueLogMaxEntries to 1000, can generate too many files.
	// These options are better configured on a usage basis, than broadly here.
	// The ValueThreshold is the most important setting a user needs to do to
	// achieve a heavier usage of LSM tree.
	// NOTE: If a user does not want to set 64KB as the ValueThreshold because
	// of performance reasons, 1KB would be a good option too, allowing
	// values smaller than 1KB to be collocated with the keys in the LSM tree.
	return DefaultOptions(path).WithValueThreshold(maxValueThreshold /* 1 MB */)
}

// WithDir returns a new Options value with Dir set to the given value.
//
// Dir is the path of the directory where key data will be stored in.
// If it doesn't exist, Badger will try to create it for you.
// This is set automatically to be the path given to `DefaultOptions`.
func (opt Options) WithDir(val string) Options {
	opt.Dir = val
	return opt
}

// WithValueDir returns a new Options value with ValueDir set to the given value.
//
// ValueDir is the path of the directory where value data will be stored in.
// If it doesn't exist, Badger will try to create it for you.
// This is set automatically to be the path given to `DefaultOptions`.
func (opt Options) WithValueDir(val string) Options {
	opt.ValueDir = val
	return opt
}

// WithSyncWrites returns a new Options value with SyncWrites set to the given value.
//
// When SyncWrites is true all writes are synced to disk. Setting this to false would achieve better
// performance, but may cause data loss in case of crash.
//
// The default value of SyncWrites is true.
func (opt Options) WithSyncWrites(val bool) Options {
	opt.SyncWrites = val
	return opt
}

// WithTableLoadingMode returns a new Options value with TableLoadingMode set to the given value.
//
// TableLoadingMode indicates which file loading mode should be used for the LSM tree data files.
//
// The default value of TableLoadingMode is options.MemoryMap.
func (opt Options) WithTableLoadingMode(val options.FileLoadingMode) Options {
	opt.TableLoadingMode = val
	return opt
}

// WithValueLogLoadingMode returns a new Options value with ValueLogLoadingMode set to the given
// value.
//
// ValueLogLoadingMode indicates which file loading mode should be used for the value log data
// files.
//
// The default value of ValueLogLoadingMode is options.MemoryMap.
func (opt Options) WithValueLogLoadingMode(val options.FileLoadingMode) Options {
	opt.ValueLogLoadingMode = val
	return opt
}

// WithNumVersionsToKeep returns a new Options value with NumVersionsToKeep set to the given value.
//
// NumVersionsToKeep sets how many versions to keep per key at most.
//
// The default value of NumVersionsToKeep is 1.
func (opt Options) WithNumVersionsToKeep(val int) Options {
	opt.NumVersionsToKeep = val
	return opt
}

// WithReadOnly returns a new Options value with ReadOnly set to the given value.
//
// When ReadOnly is true the DB will be opened on read-only mode.
// Multiple processes can open the same Badger DB.
// Note: if the DB being opened had crashed before and has vlog data to be replayed,
// ReadOnly will cause Open to fail with an appropriate message.
//
// The default value of ReadOnly is false.
func (opt Options) WithReadOnly(val bool) Options {
	opt.ReadOnly = val
	return opt
}

// WithTruncate returns a new Options value with Truncate set to the given value.
//
// Truncate indicates whether value log files should be truncated to delete corrupt data, if any.
// This option is ignored when ReadOnly is true.
//
// The default value of Truncate is false.
func (opt Options) WithTruncate(val bool) Options {
	opt.Truncate = val
	return opt
}

// WithLogger returns a new Options value with Logger set to the given value.
//
// Logger provides a way to configure what logger each value of badger.DB uses.
//
// The default value of Logger writes to stderr using the log package from the Go standard library.
func (opt Options) WithLogger(val Logger) Options {
	opt.Logger = val
	return opt
}

// WithEventLogging returns a new Options value with EventLogging set to the given value.
//
// EventLogging provides a way to enable or disable trace.EventLog logging.
//
// The default value of EventLogging is true.
func (opt Options) WithEventLogging(enabled bool) Options {
	opt.EventLogging = enabled
	return opt
}

// WithMaxTableSize returns a new Options value with MaxTableSize set to the given value.
//
// MaxTableSize sets the maximum size in bytes for each LSM table or file.
//
// The default value of MaxTableSize is 64MB.
func (opt Options) WithMaxTableSize(val int64) Options {
	opt.MaxTableSize = val
	return opt
}

// WithLevelSizeMultiplier returns a new Options value with LevelSizeMultiplier set to the given
// value.
//
// LevelSizeMultiplier sets the ratio between the maximum sizes of contiguous levels in the LSM.
// Once a level grows to be larger than this ratio allowed, the compaction process will be
//  triggered.
//
// The default value of LevelSizeMultiplier is 10.
func (opt Options) WithLevelSizeMultiplier(val int) Options {
	opt.LevelSizeMultiplier = val
	return opt
}

// WithMaxLevels returns a new Options value with MaxLevels set to the given value.
//
// Maximum number of levels of compaction allowed in the LSM.
//
// The default value of MaxLevels is 7.
func (opt Options) WithMaxLevels(val int) Options {
	opt.MaxLevels = val
	return opt
}

// WithValueThreshold returns a new Options value with ValueThreshold set to the given value.
//
// ValueThreshold sets the threshold used to decide whether a value is stored directly in the LSM
// tree or separately in the log value files.
//
// The default value of ValueThreshold is 32, but LSMOnlyOptions sets it to maxValueThreshold.
func (opt Options) WithValueThreshold(val int) Options {
	opt.ValueThreshold = val
	return opt
}

// WithNumMemtables returns a new Options value with NumMemtables set to the given value.
//
// NumMemtables sets the maximum number of tables to keep in memory before stalling.
//
// The default value of NumMemtables is 5.
func (opt Options) WithNumMemtables(val int) Options {
	opt.NumMemtables = val
	return opt
}

// WithBloomFalsePositive returns a new Options value with BloomFalsePositive set
// to the given value.
//
// BloomFalsePositive sets the false positive probability of the bloom filter in any SSTable.
// Before reading a key from table, the bloom filter is checked for key existence.
// BloomFalsePositive might impact read performance of DB. Lower BloomFalsePositive value might
// consume more memory.
//
// The default value of BloomFalsePositive is 0.01.
func (opt Options) WithBloomFalsePositive(val float64) Options {
	opt.BloomFalsePositive = val
	return opt
}

// WithBlockSize returns a new Options value with BlockSize set to the given value.
//
// BlockSize sets the size of any block in SSTable. SSTable is divided into multiple blocks
// internally. Each block is compressed using prefix diff encoding.
//
// The default value of BlockSize is 4KB.
func (opt Options) WithBlockSize(val int) Options {
	opt.BlockSize = val
	return opt
}

// WithNumLevelZeroTables returns a new Options value with NumLevelZeroTables set to the given
// value.
//
// NumLevelZeroTables sets the maximum number of Level 0 tables before compaction starts.
//
// The default value of NumLevelZeroTables is 5.
func (opt Options) WithNumLevelZeroTables(val int) Options {
	opt.NumLevelZeroTables = val
	return opt
}

// WithNumLevelZeroTablesStall returns a new Options value with NumLevelZeroTablesStall set to the
// given value.
//
// NumLevelZeroTablesStall sets the number of Level 0 tables that once reached causes the DB to
// stall until compaction succeeds.
//
// The default value of NumLevelZeroTablesStall is 10.
func (opt Options) WithNumLevelZeroTablesStall(val int) Options {
	opt.NumLevelZeroTablesStall = val
	return opt
}

// WithLevelOneSize returns a new Options value with LevelOneSize set to the given value.
//
// LevelOneSize sets the maximum total size for Level 1.
//
// The default value of LevelOneSize is 20MB.
func (opt Options) WithLevelOneSize(val int64) Options {
	opt.LevelOneSize = val
	return opt
}

// WithValueLogFileSize returns a new Options value with ValueLogFileSize set to the given value.
//
// ValueLogFileSize sets the maximum size of a single value log file.
//
// The default value of ValueLogFileSize is 1GB.
func (opt Options) WithValueLogFileSize(val int64) Options {
	opt.ValueLogFileSize = val
	return opt
}

// WithValueLogMaxEntries returns a new Options value with ValueLogMaxEntries set to the given
// value.
//
// ValueLogMaxEntries sets the maximum number of entries a value log file can hold approximately.
// A actual size limit of a value log file is the minimum of ValueLogFileSize and
// ValueLogMaxEntries.
//
// The default value of ValueLogMaxEntries is one million (1000000).
func (opt Options) WithValueLogMaxEntries(val uint32) Options {
	opt.ValueLogMaxEntries = val
	return opt
}

// WithNumCompactors returns a new Options value with NumCompactors set to the given value.
//
// NumCompactors sets the number of compaction workers to run concurrently.
// Setting this to zero stops compactions, which could eventually cause writes to block forever.
//
// The default value of NumCompactors is 2.
func (opt Options) WithNumCompactors(val int) Options {
	opt.NumCompactors = val
	return opt
}

// WithCompactL0OnClose returns a new Options value with CompactL0OnClose set to the given value.
//
// CompactL0OnClose determines whether Level 0 should be compacted before closing the DB.
// This ensures that both reads and writes are efficient when the DB is opened later.
// CompactL0OnClose is set to true if KeepL0InMemory is set to true.
// The default value of CompactL0OnClose is true.
func (opt Options) WithCompactL0OnClose(val bool) Options {
	opt.CompactL0OnClose = val
	return opt
}

// WithLogRotatesToFlush returns a new Options value with LogRotatesToFlush set to the given value.
//
// LogRotatesToFlush sets the number of value log file rotates after which the Memtables are
// flushed to disk. This is useful in write loads with fewer keys and larger values. This work load
// would fill up the value logs quickly, while not filling up the Memtables. Thus, on a crash
// and restart, the value log head could cause the replay of a good number of value log files
// which can slow things on start.
//
// The default value of LogRotatesToFlush is 2.
func (opt Options) WithLogRotatesToFlush(val int32) Options {
	opt.LogRotatesToFlush = val
	return opt
}

// WithEncryptionKey return a new Options value with EncryptionKey set to the given value.
//
// EncryptionKey is used to encrypt the data with AES. Type of AES is used based on the key
// size. For example 16 bytes will use AES-128. 24 bytes will use AES-192. 32 bytes will
// use AES-256.
func (opt Options) WithEncryptionKey(key []byte) Options {
	opt.EncryptionKey = key
	return opt
}

// WithEncryptionRotationDuration returns new Options value with the duration set to
// the given value.
//
// Key Registry will use this duration to create new keys. If the previous generated
// key exceed the given duration. Then the key registry will create new key.
func (opt Options) WithEncryptionKeyRotationDuration(d time.Duration) Options {
	opt.EncryptionKeyRotationDuration = d
	return opt
}

// WithKeepL0InMemory returns a new Options value with KeepL0InMemory set to the given value.
//
// When KeepL0InMemory is set to true we will keep all Level 0 tables in memory. This leads to
// better performance in writes as well as compactions. In case of DB crash, the value log replay
// will take longer to complete since memtables and all level 0 tables will have to be recreated.
// This option also sets CompactL0OnClose option to true.
//
// The default value of KeepL0InMemory is true.
func (opt Options) WithKeepL0InMemory(val bool) Options {
	opt.KeepL0InMemory = val
	return opt
}

// WithCompressionType returns a new Options value with CompressionType set to the given value.
//
// When compression type is set, every block will be compressed using the specified algorithm.
// This option doesn't affect existing tables. Only the newly created tables will be compressed.
func (opt Options) WithCompressionType(cType options.CompressionType) Options {
	opt.Compression = cType
	return opt
}

// WithVerifyValueChecksum returns a new Options value with VerifyValueChecksum set to
// the given value.
//
// When VerifyValueChecksum is set to true, checksum will be verified for every entry read
// from the value log. If the value is stored in SST (value size less than value threshold) then the
// checksum validation will not be done.
//
// The default value of VerifyValueChecksum is False.
func (opt Options) WithVerifyValueChecksum(val bool) Options {
	opt.VerifyValueChecksum = val
	return opt
}

// WithMaxCacheSize returns a new Options value with MaxCacheSize set to the given value.
//
// This value specifies how much data cache should hold in memory. A small size of cache means lower
// memory consumption and lookups/iterations would take longer.
func (opt Options) WithMaxCacheSize(size int64) Options {
	opt.MaxCacheSize = size
	return opt
}
