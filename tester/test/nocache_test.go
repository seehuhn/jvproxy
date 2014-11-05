package test

// compile time test: `NoCache` implements the `Test` interface
var _ Test = &NoCache{}
