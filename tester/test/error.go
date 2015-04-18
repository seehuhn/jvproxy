package test

type brokenTest string

var exMissingRequest = brokenTest("missing call to ForwardRequest")
var exMissingResponse = brokenTest("missing call to ForwardResponse")

type brokenProxy string

var exWrongPath = brokenProxy("wrong request path forwarded")

type testFailure string
type testSuccess string
