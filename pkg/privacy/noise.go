package privacy

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// NoiseGenerator provides differential privacy noise generation
type NoiseGenerator struct {
	rng *rand.Rand
}

// NewNoiseGenerator creates a new noise generator
func NewNoiseGenerator() *NoiseGenerator {
	return &NoiseGenerator{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// LaplaceNoise generates Laplace noise for numerical aggregates
// scale = sensitivity / epsilon
func (ng *NoiseGenerator) LaplaceNoise(scale float64) float64 {
	// Laplace distribution: f(x) = (1/2b) * exp(-|x|/b)
	// where b is the scale parameter

	// Generate uniform random number in (-0.5, 0.5)
	u := ng.rng.Float64() - 0.5

	// Inverse CDF of Laplace distribution
	if u < 0 {
		return scale * math.Log(1.0+2.0*u)
	}
	return -scale * math.Log(1.0-2.0*u)
}

// GeometricNoise generates geometric noise for count queries
// p = 1 - exp(-epsilon)
func (ng *NoiseGenerator) GeometricNoise(epsilon float64) int {
	if epsilon <= 0 {
		return 0
	}

	p := 1.0 - math.Exp(-epsilon)

	// Generate geometric noise
	// P(X=k) = (1-p)^|k| * p for k in Z

	// Generate sign (positive or negative)
	sign := 1
	if ng.rng.Float64() < 0.5 {
		sign = -1
	}

	// Generate magnitude using inverse CDF
	u := ng.rng.Float64()
	magnitude := int(math.Floor(math.Log(u) / math.Log(1-p)))

	return sign * magnitude
}

// GaussianNoise generates Gaussian noise for (ε,δ)-differential privacy
// sigma = sqrt(2*ln(1.25/δ)) * sensitivity / epsilon
func (ng *NoiseGenerator) GaussianNoise(epsilon, delta, sensitivity float64) float64 {
	if epsilon <= 0 || delta <= 0 {
		return 0
	}

	// Calculate standard deviation for Gaussian mechanism
	sigma := math.Sqrt(2*math.Log(1.25/delta)) * sensitivity / epsilon

	// Generate Gaussian noise using Box-Muller transform
	u1 := ng.rng.Float64()
	u2 := ng.rng.Float64()

	z0 := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)

	return sigma * z0
}

// RandomizedResponse implements randomized response technique
// probability: probability of truthful response (should be > 0.5)
func (ng *NoiseGenerator) RandomizedResponse(trueValue bool, probability float64) bool {
	if probability <= 0.5 || probability >= 1.0 {
		// Default to 0.75 if invalid probability
		probability = 0.75
	}

	if ng.rng.Float64() < probability {
		return trueValue
	}
	return !trueValue
}

// CalculateSensitivity calculates L1 sensitivity for different query types
func CalculateSensitivity(queryType string, params map[string]interface{}) float64 {
	switch queryType {
	case "COUNT":
		return 1.0 // Adding/removing one record changes count by at most 1
	case "SUM":
		if maxVal, ok := params["max_value"].(float64); ok {
			return maxVal // Sensitivity is the maximum possible value
		}
		return 1000.0 // Default max value
	case "AVG":
		if maxVal, ok := params["max_value"].(float64); ok {
			return maxVal // Sensitivity depends on data range
		}
		return 1000.0 // Default max value
	case "STDDEV":
		if maxVal, ok := params["max_value"].(float64); ok {
			return maxVal * 2 // Standard deviation has higher sensitivity
		}
		return 2000.0 // Default max value
	default:
		return 1.0
	}
}

// NoiseConfig holds configuration for noise generation
type NoiseConfig struct {
	Epsilon     float64 // Privacy parameter
	Delta       float64 // Failure probability (for Gaussian mechanism)
	Sensitivity float64 // Query sensitivity
	Mechanism   string  // "laplace", "gaussian", "geometric"
}

// GenerateNoise generates appropriate noise based on configuration
func (ng *NoiseGenerator) GenerateNoise(config NoiseConfig) (interface{}, error) {
	switch config.Mechanism {
	case "laplace":
		scale := config.Sensitivity / config.Epsilon
		return ng.LaplaceNoise(scale), nil
	case "gaussian":
		return ng.GaussianNoise(config.Epsilon, config.Delta, config.Sensitivity), nil
	case "geometric":
		return ng.GeometricNoise(config.Epsilon), nil
	default:
		return 0, fmt.Errorf("unsupported noise mechanism: %s", config.Mechanism)
	}
}
