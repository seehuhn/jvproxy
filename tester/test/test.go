package test

type Case func(Helper, ...interface{})

type cacheProperties uint16

const (
	isCaching cacheProperties = 1 << iota
	doesRevalidate
)
