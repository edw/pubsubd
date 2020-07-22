# pubsubd

Pubsubd is a simple pub-sub server with a curl-friendly HTTP interface. All messages are posted to a single topic. Subscriptions are creared implicitly by performing a pull or ack operation. Pubsubd is poll-only: it does not support push operations.

## Installing

```
$ go install poseur.com/pubsubd
```

## Running

```
$ pubsubd --data-dir ./data --host 127.0.0.1 --port 8080
```

## Subscribing

```
$ curl "http://localhost:8080/pull?sub=SUBNAME&n=0"
```

## Sending messages

```
$ curl -X POST -D - \
    -d "message=foo&message=bar&message=42" \
    "http://localhost:8080/send"
```

## Getting oldest unacknowledged messages

```
$ curl -D - "http://localhost:8080/pull?sub=SUBNAME&n=10"
```

Output:

```
HTTP/1.1 200 OK
Date: Wed, 22 Jul 2020 18:25:47 GMT
Content-Length: 58
Content-Type: text/plain; charset=utf-8

{"n_messages":3,"messages":{"0":"foo","1":"bar","2":"42"}}
```

## Acknowledging messages

```
$ curl -X POST -D - "http://localhost:8080/ack?sub=SUBNAME&id=0"
```

This will result in another pull on sub `SUBNAME` excluding message id 0:

```
$ curl -D - "http://localhost:8080/pull?sub=SUBNAME&n=10"
```

Output:

```
HTTP/1.1 200 OK
Date: Wed, 22 Jul 2020 18:29:06 GMT
Content-Length: 48
Content-Type: text/plain; charset=utf-8

{"n_messages":2,"messages":{"1":"bar","2":"42"}}
```

## Unsubscribing

```
$ curl -X POST -D - "http://localhost:8080/unsub?sub=SUBNAME"
```

A subsequent pull shows that there are no longer any waiting messages:


```
$ curl -D - "http://localhost:8080/pull?sub=SUBNAME&n=10"
```

Output:

```
HTTP/1.1 200 OK
Date: Wed, 22 Jul 2020 18:32:09 GMT
Content-Length: 30
Content-Type: text/plain; charset=utf-8

{"n_messages":0,"messages":{}}
```

Of course, that pull operation re-creeated the subscription, so be careful out  there!

## Testing

There is an included `test.sh` script that will fire up an instance of pubsubd and perform operations similar to the above to verify something approximating proper operation. The script assumes that the `pubsubd` binary exists in same directory. 