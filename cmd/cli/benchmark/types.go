package benchmark

import (
	"time"

	"github.com/fystack/mpcium/pkg/types"
)

type BenchmarkResult struct {
	TotalOperations  int
	SuccessfulOps    int
	FailedOps        int
	TotalTime        time.Duration
	AverageTime      time.Duration
	MedianTime       time.Duration
	OperationTimes   []time.Duration
	ErrorRate        float64
	OperationsPerMin float64
	BatchSize        int
	TotalBatches     int
	BatchTimes       []time.Duration
}

type OperationResult struct {
	ID          string
	StartTime   time.Time
	EndTime     time.Time
	Completed   bool
	Success     bool
	ErrorCode   types.ErrorCode
	ErrorReason string
}
