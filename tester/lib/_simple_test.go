package test

// compile time test: `Simple` implements the `Test` interface
var _ Test = &Simple{}
