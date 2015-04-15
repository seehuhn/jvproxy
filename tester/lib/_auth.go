package test

// AuthTest exercises the requirements of RFC7234, section 3.2
// (Storing Responses to Authenticated Requests).
type AuthTest struct{}

func NewAuthTest() *AuthTest {
	return &AuthTest{}
}

func (t *AuthTest) Info() *Info {
	return &Info{
		Name: "Auth",
		RFC:  "7234-3.2",
	}
}

func (t *AuthTest) Test(h Helper) {
	secret := UniqueString(8)

	req := h.NewRequest("GET")
	req.Header.Add("Authorization", secret)
	_, req = h.ForwardRequest(req)
	if req.Header.Get("Authorization") != secret {
		h.Fail("wrong/missing Authorization header")
	}

	h.ForwardResponse()

	req = h.NewRequest("GET")
	_, req = h.ForwardRequest(req)
	if req == nil {
		h.Fail("proxy did not revalidate authenticated response")
	}

	h.ForwardResponse()
}
