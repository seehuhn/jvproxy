package test

// A Case represents a single unit test for the proxy.  The Helper
// object should be used to generate HTTP requests and responses, and
// to report the test outcome.  Test cases can only be run via
// Runner.Run().  The optional arguments used in the call to
// Runner.Run() are passed through to the test case without change.
type Case func(Helper, ...interface{})
