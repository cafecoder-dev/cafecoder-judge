# cafecoder-judge
cafecoder のジャッジシステム

## Usage
1. [https://github.com/cafecoder-dev/cafecoder-container-client] から clone して `README.md` に従ってください。
2. `.env.sample` に従って `.env` ファイルを作成してください。
3. `key.json` を作成を作成してください。(gcp 関係)
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
