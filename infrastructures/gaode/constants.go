package gaode

// Driving strategy constants
type DrivingStrategy int

const (
	StrategySpeedFirst      DrivingStrategy = 0  // Speed priority
	StrategyCostFirst       DrivingStrategy = 1  // Cost priority
	StrategyFastestNormal   DrivingStrategy = 2  // Regular fastest
	StrategyRecommended     DrivingStrategy = 32 // Default recommended
	StrategyAvoidCongestion DrivingStrategy = 33 // Avoid congestion
	StrategyHighwayFirst    DrivingStrategy = 34 // Highway priority
	StrategyNoHighway       DrivingStrategy = 35 // No highway
	StrategyLessToll        DrivingStrategy = 36 // Less toll
	StrategyMainRoad        DrivingStrategy = 37 // Main road priority
)

// Sort rule constants
const (
	SortByDistance = "distance" // Sort by distance
	SortByWeight   = "weight"   // Sort by comprehensive weight
)

// Car type constants
const (
	CarTypeGasoline = 0 // Regular gasoline car
	CarTypeElectric = 1 // Pure electric car
	CarTypeHybrid   = 2 // Plugin hybrid car
)

// Extensions type for regeocode
const (
	ExtensionsBase = "base" // Return basic info only
	ExtensionsAll  = "all"  // Return all available info
)
