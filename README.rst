JvProxy
=======

An experimental caching web proxy.

Copyright (C) 2014  Jochen Voss

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Summary
-------

This is work in progress.  Currently the code is not very
configurable, it always starts a web proxy listening on
``localhost:8080``.  For use by the cache, the proxy creates a new
directory ``cache/`` inside the current directory.

TODO
----

- implement more of RFC 2616 and RFC 7234
- restructure and modularize the code
- add more unit tests
- optimise speed for concurrent access
- add a mechanism for pruning the cache
- try to handle HTTPS

REFERENCES
----------

- http://tools.ietf.org/html/rfc2616
- http://tools.ietf.org/html/rfc7234
- https://github.com/elazarl/goproxy
- http://golang.org/src/pkg/net/http/httputil/reverseproxy.go?s=2534:2609#L87
