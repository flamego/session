# session

[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/flamego/session/go.yml?branch=main&logo=github&style=for-the-badge)](https://github.com/flamego/session/actions?query=workflow%3AGo)
[![GoDoc](https://img.shields.io/badge/GoDoc-Reference-blue?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/flamego/session?tab=doc)
[![Sourcegraph](https://img.shields.io/badge/view%20on-Sourcegraph-brightgreen.svg?style=for-the-badge&logo=sourcegraph)](https://sourcegraph.com/github.com/flamego/session)

Package session is a middleware that provides the session management for [Flamego](https://github.com/flamego/flamego).

## Installation

	go get github.com/flamego/session

## Getting started

```go
package main

import (
	"github.com/flamego/flamego"
	"github.com/flamego/session"
)

func main() {
	f := flamego.Classic()
	f.Use(session.Sessioner())
	f.Get("/", func(s session.Session) {
		s.Set("user_id", 123)
		userID, ok := s.Get("user_id").(int)
		// ...
	})
	f.Run()
}
```

## Getting help

- Read [documentation and examples](https://flamego.dev/middleware/session.html).
- Please [file an issue](https://github.com/flamego/flamego/issues) or [start a discussion](https://github.com/flamego/flamego/discussions) on the [flamego/flamego](https://github.com/flamego/flamego) repository.

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
