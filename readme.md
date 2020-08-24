# cafecoder-judge
cafecoder のジャッジシステム

## Usage
0. [https://github.com/cafecoder-dev/cafecoder-container-client] から clone して `README.md` に従ってください。
1. `.env` ファイルを作成
2. `key.json` を作成(gcp 関係)
3. 次のコマンドを実行してビルド
```console
$ cd src/cmd/cafecoder-judge
$ make
$ cd ./../../..
```
4. 管理者権限でコマンドを実行。本番環境だったら2つ目のコマンドを実行。
```console
# ./cafecoder-judge
```
```console
# nohup ./cafecoder-judge 2&>> log &
```
