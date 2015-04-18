package test

type brokenTest string

var exMissingRequest = brokenTest("missing call to SendRequestToServer")
var exMissingResponse = brokenTest("missing call to SendResponseToClient")

type brokenProxy string

var exWrongPath = brokenProxy("wrong request path forwarded")

type testFailure string
type testSuccess string
