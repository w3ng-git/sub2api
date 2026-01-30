//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTransferCacheTokens_ZeroRatio 测试 ratio=0 时不转移
func TestTransferCacheTokens_ZeroRatio(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(100, 1000, 0)
	require.Equal(t, 100, newCreation)
	require.Equal(t, 1000, newRead)
}

// TestTransferCacheTokens_NegativeRatio 测试负数 ratio 不转移
func TestTransferCacheTokens_NegativeRatio(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(100, 1000, -0.1)
	require.Equal(t, 100, newCreation)
	require.Equal(t, 1000, newRead)
}

// TestTransferCacheTokens_ZeroCacheRead 测试 cacheRead=0 时不转移
func TestTransferCacheTokens_ZeroCacheRead(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(100, 0, 0.2)
	require.Equal(t, 100, newCreation)
	require.Equal(t, 0, newRead)
}

// TestTransferCacheTokens_TenPercent 测试 10% 转移
func TestTransferCacheTokens_TenPercent(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(100, 1000, 0.1)
	// 转移 1000 * 0.1 = 100
	require.Equal(t, 200, newCreation) // 100 + 100
	require.Equal(t, 900, newRead)     // 1000 - 100
}

// TestTransferCacheTokens_FifteenPercent 测试 15% 转移
func TestTransferCacheTokens_FifteenPercent(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(50, 2000, 0.15)
	// 转移 2000 * 0.15 = 300
	require.Equal(t, 350, newCreation) // 50 + 300
	require.Equal(t, 1700, newRead)    // 2000 - 300
}

// TestTransferCacheTokens_TwentyPercent 测试 20% 转移
func TestTransferCacheTokens_TwentyPercent(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(0, 5000, 0.2)
	// 转移 5000 * 0.2 = 1000
	require.Equal(t, 1000, newCreation) // 0 + 1000
	require.Equal(t, 4000, newRead)     // 5000 - 1000
}

// TestTransferCacheTokens_FullTransfer 测试 100% 转移
func TestTransferCacheTokens_FullTransfer(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(200, 800, 1.0)
	// 转移全部 800
	require.Equal(t, 1000, newCreation) // 200 + 800
	require.Equal(t, 0, newRead)        // 800 - 800
}

// TestTransferCacheTokens_OverOneRatio 测试 ratio > 1 时限制为 1
func TestTransferCacheTokens_OverOneRatio(t *testing.T) {
	newCreation, newRead := TransferCacheTokens(100, 500, 1.5)
	// ratio 被限制为 1，转移全部 500
	require.Equal(t, 600, newCreation) // 100 + 500
	require.Equal(t, 0, newRead)       // 500 - 500
}

// TestTransferCacheTokens_TotalPreserved 测试总数不变
func TestTransferCacheTokens_TotalPreserved(t *testing.T) {
	testCases := []struct {
		creation int
		read     int
		ratio    float64
	}{
		{0, 1000, 0.1},
		{100, 900, 0.2},
		{500, 500, 0.5},
		{1000, 0, 0.3},
		{123, 456, 0.15},
	}

	for _, tc := range testCases {
		originalTotal := tc.creation + tc.read
		newCreation, newRead := TransferCacheTokens(tc.creation, tc.read, tc.ratio)
		newTotal := newCreation + newRead
		require.Equal(t, originalTotal, newTotal, "total should be preserved for creation=%d read=%d ratio=%f", tc.creation, tc.read, tc.ratio)
	}
}

// TestTransferCacheTokens_NonNegative 测试结果非负
func TestTransferCacheTokens_NonNegative(t *testing.T) {
	testCases := []struct {
		creation int
		read     int
		ratio    float64
	}{
		{0, 0, 0.5},
		{0, 100, 1.0},
		{100, 0, 0.5},
		{-10, 100, 0.5}, // 边界情况：负数 creation
	}

	for _, tc := range testCases {
		newCreation, newRead := TransferCacheTokens(tc.creation, tc.read, tc.ratio)
		require.GreaterOrEqual(t, newCreation, tc.creation, "newCreation should be >= original for creation=%d read=%d ratio=%f", tc.creation, tc.read, tc.ratio)
		require.GreaterOrEqual(t, newRead, 0, "newRead should be >= 0 for creation=%d read=%d ratio=%f", tc.creation, tc.read, tc.ratio)
	}
}
