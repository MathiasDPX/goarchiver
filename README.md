# goarchiver

goarchiver is an lightweight archiving tool written in Go. 

## Features

- Fast creation of archives
- SimpleCLI
- mitmproxy addon for replaying archives

## Installation

### **From source**

```
go install github.com/MathiasDPX/goarchiver@latest
```

## Usage

```
goarchiver -start https://mathiasd.fr/ -whitelist mathiasd.fr
```

This command crawls the provided URLs, follows links limited to the whitelisted domain (to prevent escape to big sites like github) and generate a .warc.gz

You can provide multiple starting points and whitelisted domains by adding a comma between them like this

```
goarchiver -start https://thevalleyofcode.com/ -whitelist thevalleyofcode.com,fonts.googleapis.com,fonts.gstatic.com
```

## mitmproxy addon

A simple mitmproxy addon is provided in [/replay](/replay/). However, as it require warcio to read the archive you'll need to run mitmproxy via [uv](https://docs.mitmproxy.org/stable/overview/installation/#installation-from-the-python-package-index-pypi)