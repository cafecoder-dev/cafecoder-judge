# cafecoder-judge
cafecoder-judge です。以下備忘録です。

## 依存関係
+ Go
+ Docker

## judge 起動コマンド
```sh
$ sudo su root
# nohup ./judge 2&>>log &
```

## docker build コマンド
```sh
$ sudo docker build -t cafecoder:<version> .
```