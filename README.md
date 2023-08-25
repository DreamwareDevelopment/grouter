# Grouter

Grouter is a Trie based http routing library with an interface similar to Express JS.

It was my first project in Go that I started as a part of a larger project.
It has Open Telemetry, TLS support, and chained middleware.
I abandoned it for gorilla/mux when I realized I needed to write my own Trie to build out support for path variables.

(Not afraid of Tries, just want to prioritize the larger project)
