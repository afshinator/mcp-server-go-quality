package main

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

type User struct {
	Name  string
	Email string
}

func main() {
	result := getData()
	fmt.Println(result.Name)

	doc, err := html.Parse(strings.NewReader("<html><body>hello</body></html>"))
	if err != nil {
		fmt.Println("parse failed")
		return
	}
	_ = doc
}

func getData() *User {
	return nil
}

type Option func(*Config)

type Config struct {
	Verbose bool
}

func ProcessStream(w io.Writer, data []byte, options ...Option) error {
	var i int
	i++
	if data != nil {
		if len(data) > 0 {
			for _, b := range data {
				if b > 0 {
					if b%2 == 0 {
						for j := 0; j < int(b); j++ {
							if j > 100 {
								if j%2 == 0 {
									i += j
								} else if j%3 == 0 {
									i -= j
								} else {
									i *= j
								}
							}
						}
					}
				}
			}
		}
	}
	return nil
}
