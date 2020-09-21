# cafecoder-judge
cafecoder のジャッジシステム

## Requirements
+ Linux
+ Go (1.15)
+ Docker (19.03)

## Usage
1. [https://github.com/cafecoder-dev/cafecoder-container-client] を clone して Docker image を作成してください。
2. `.env.sample` に従って `.env` ファイルを作成してください。
3. `key.json` (gcp のキーファイル)を作成してください。
4. 3344 ポートを開放してください。コンテナと tcp 通信をするためです。  
5. 次のコマンドを実行してビルドしてください。
```console
$ cd src/cmd/cafecoder-judge
$ make
$ cd ./../../..
```
6. 管理者権限でコマンドを実行してください。本番環境だったら2つ目のコマンドを実行してください。
```console
# ./cafecoder-judge
```
```console
# nohup ./cafecoder-judge 2&>> log &
```