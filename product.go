package main

type Product interface {
	Name() string
	Type() string
	Probability() float64
}

type FluidProduct struct {
	Amount      float64
	AmountMin   float64
	AmountMax   float64
	Temperature float64
}

type ItemProduct struct {
	Amount    float64
	AmountMin float64
	AmountMax float64
}
