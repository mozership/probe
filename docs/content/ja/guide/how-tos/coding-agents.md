# コーディングエージェントと使う

コーディングエージェントはサービスを変更するとき、その変更が正しいことを示すユニットテストも自分で書きます。
コードとテストは課題の同じ解釈から書かれるため、解釈が誤っていれば、誤ったコードが誤ったテストを通ります。
Probeのワークフローは、変更を外側から確かめます。
動いているサービスを、クライアントが使うのと同じプロトコルで操作し、返ってきた応答、書き込まれた行、送信されたメールを検証します。

このガイドでは、エージェントが変更のたびにワークフローを書いて実行し、その結果を人が信頼できるように、プロジェクトを設定します。
設定が終わると、次の3つができるようになります。ワークフローを実行前に検査すること、サービスとそのデータベースに対して実行すること、書き込んではいけない環境で実行することです。

<img class="diagram" src="/demo-ja.svg" alt="3場面の端末。probe checkがAIエージェントの書いたE2Eテストの誤りを報告し、直したテストがAPIを呼んで書き込まれた行を問い合わせ、--read-onlyではPOSTが拒否される。" />

## エージェントの作業環境にProbeを入れる

エージェントは、Probeが入っていない新しいコンテナで作業することがよくあります。
次のうち、コンテナにあるほうでインストールします。

```bash
go install github.com/mozership/probe/cmd/probe@latest
brew install mozership/tap/probe
```

サンドボックスにセットアップスクリプトがあればそこに、なければ後述の指示の中にこのコマンドを書きます。
HomebrewもGoもない環境では、リリースをダウンロードしてチェックサムを検証するコマンドが[インストール](/ja/guide/introduction/installation#3-リリースのダウンロード)にあります。

## スキルを入れる

スキルは、ワークフローの書き方、検査の仕方、失敗の読み方をエージェントに教えます。
スキルはバイナリに組み込まれており、同じく組み込みのリファレンスである`probe guide`を読むようエージェントに指示します。
そのためエージェントは、別のバージョンについての記憶ではなく、インストールされているバージョンに合わせてYAMLを書きます。

```bash
probe skill install                        # .claude/skills/probe/SKILL.md（Claude Code向け）
probe skill install .agents/skills/probe   # エージェントがスキルを読む、ほかのディレクトリ
probe skill                                # 標準出力に出す（別の場所に置く場合）
```

書き出されたファイルはコミットし、Probeを更新したら同じコマンドをもう一度実行します。

## いつ使うのかを指示する

スキルが教えるのはProbeの使い方です。
いつ使うのかは、`AGENTS.md`や`CLAUDE.md`など、エージェントが指示を読むファイルに書きます。

```markdown
## End-to-endの確認

- サービスがネットワーク越しに行うこと（エンドポイント、クエリ、送信するメール）を変更したら、動いているサービスに対してその変更を確かめるProbeのワークフローを`probe/`に追加するか、既存のものを更新する。
- YAMLを書く前に`probe guide`を読む。パラメータを推測で書かない。
- `probe check <file>`を実行してエラーをすべて直してから、`probe --report markdown=probe-report.md <file>`を実行する。
- ワークフローが通ったら変更は完了とする。`probe-report.md`をプルリクエストに載せる。
- `probe`が入っていなければ、`brew install mozership/tap/probe`か`go install github.com/mozership/probe/cmd/probe@latest`で入れる。
```

ディレクトリと条件はプロジェクトに合わせて変えてください。
ワークフローには接続する相手が必要なので、`docker compose up -d`のようなサービスの起動方法も書いておきます。

## エージェントが行う手順

スキルと指示があれば、変更は次の手順で進みます。

1. エージェントは`probe guide`の必要なページを読み、ワークフローを書くか更新します。
2. `probe check`がワークフローを実行せずに読みます。`test`を`tset`と書いたような、実行時には無視されるキーは行番号つきのエラーになり、何も確かめていないステップは警告になります。
3. `probe`がワークフローを実行します。終了コードで調べる場所がわかります。`1`はtestの失敗か仕様の違反、`2`はワークフロー自体の誤り、`3`は相手が応答しなかったことを示します。
4. 失敗したとき、エージェントはMarkdownのレポートを読みます。レポートには、失敗したステップごとに、test、失敗の種類、リクエストと応答が載っています。エージェントはそれをもとに、サービスかワークフローを直します。

```bash
probe check probe/signup.yml
probe --report markdown=probe-report.md probe/signup.yml
```

## 正しさをエージェントではなく仕様で判定する

エージェントが書いた`test`は、エージェントが誤って実装した応答と一致してしまうことがあります。
`openapi`を指定すると、httpアクションはリクエストと応答をOpenAPIドキュメントと照合するので、正しさはドキュメントで決まります。
grpcアクションでは、`proto`が`.proto`ファイルを使って同じ照合を行います。

```yaml
- name: Users
  defaults:
    http:
      url: "{{vars.api_url}}"
      openapi:
        spec: ./openapi.yml
  steps:
  - name: Read a user
    uses: http
    with:
      get: /users/1
    test: res.body.name != ""
```

実行のあと`probe coverage`を使うと、ドキュメントのどのオペレーションと応答をステップが確かめたか、どれが残っているかがわかります。

```bash
probe --report json=probe-report.json probe/users.yml
probe coverage openapi.yml probe-report.json
```

パラメータは[OpenAPIによる検証](/ja/reference/actions/http#openapiによる検証)を参照してください。

## エージェントが書いたワークフローを安全に実行する

まだ読んでいないワークフローを、変更されては困るシステムに対して実行するときは、ガードを設定します。
`--read-only`は書き込みを、`--allow-host`は指定していないホストへの接続を、どちらも送信する前に拒否します。

```bash
probe --read-only --allow-host api.staging.example.com probe/users.yml
```

拒否されたステップは`refused`という種類で失敗し、実行は終了コード2で終わります。
ガードはProbeを実行する人が設定するもので、ワークフローの記述で緩めることはできません。
スキルも、ガードを外したり広げたりしないようエージェントに指示しています。
`shell`のように、これから何をするのかをアクション自身が判定できないものは、`--allow-action`で指定しない限りガードの下では拒否されます。
各アクションが何を拒否するか、ガードで防げないものは何かは、[ガード](/ja/guide/concepts/guard)で説明しています。

## 同じワークフローをCIで実行する

エージェントが実行したワークフローはリポジトリ内のファイルなので、CIがプルリクエストでもう一度実行できます。
レポートはジョブサマリーに出力され、レビューする人が読めます。

```yaml
name: E2E
on: [pull_request]
jobs:
  probe:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - run: docker compose up -d --wait
    - uses: mozership/probe-action@v1.3.0
      with:
        path: probe/signup.yml
        options: --report github-summary
```

レビューする人は、コードの差分と並べて、サービスが何をしたかをステップごとに読めます。
