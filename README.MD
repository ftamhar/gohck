# Go Health Slack Bot (gohck)

Bot Slack

## Description

Bot slack yang sudah menerapkan pattern simple semaphore, berguna untuk mengirim notifikasi di slack.

## Getting Started

### Dependencies

* go version 1.19+

### Installing

```sh
go install github.com/ftamhar/gohck
```

### Executing program

* copy template config.yml
```yml
slackapikey: <SLACK_API_KEY>
slackchannel: "#test"
delayreport: 30 # minute
delaycheck: 1 # minute
worker: 5

urls:
  - https://www.google.com
  - https://github.com
  - https://gitlab.com

```

* run the program
```sh
gohck -c <config.yml location>
```
