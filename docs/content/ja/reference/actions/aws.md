# AWSアクション

AWSアクションは、AWSのサービスと、そのプロトコルに対応したサービスを呼び出します。リクエストにはそれぞれSignature Version 4で署名します。対象は、S3、MinIO、Cloudflare R2のオブジェクト、SQSやElasticMQのメッセージ、そしてAPI Gateway、Lambdaの関数URL、OpenSearchのような、HTTPで送るAWSのそのほかのAPIです。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-aws](https://github.com/mozership/probe-aws)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.24.0以降が必要です。例の`<commit SHA>`には、固定するコミットを書きます。最新リリースの`uses`の行は`probe guide aws`が表示し、各リリースのノートにもあります。

## 基本的な構文

ワークフローでは40文字のコミットSHAでアクションを固定し、[`actions`](/ja/reference/yaml-configuration#actions)で一度だけ名前を付けます。

```yaml
name: Export
actions:
  aws: github.com/mozership/probe-aws@<commit SHA>
vars:
  queue_url: "{{QUEUE_URL}}"
jobs:
- name: export
  defaults:
    aws:
      region: ap-northeast-1
  steps:
  - name: Ask the API for an export
    uses: http
    with:
      post: https://api.example.com/exports
    test: res.code == 202

  - name: Wait for the job it queues
    id: job
    uses: aws
    with:
      sqs:
        receive: "{{vars.queue_url}}"
        wait: 20s
    test: res.count > 0
    retry:
      max_attempts: 30
    outputs:
      key: res.messages[0].body.key

  - name: The export is in the bucket
    uses: aws
    with:
      s3:
        bucket: example-exports
        head: "{{outputs.job.key}}"
    test: res.code == 200 && res.size > 0
```

## パラメータ

ステップは、`with`の次のキーのどれか1つで1つのサービスを呼び出します。キーの値には、何をするかを書きます。

| キー | 呼び出すもの |
|---|---|
| [`s3`](#s3) | S3と、そのプロトコルに対応したストレージのオブジェクト |
| [`sqs`](#sqs) | SQSと、そのJSONプロトコルに対応したキューのメッセージ |
| [`request`](#request) | HTTPで送るAWSの任意のAPI。リクエストは自分で書きます |

残りのパラメータは、どのサービスでも共通に、サービスの場所と署名の方法を指定します。

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `region` | String | No | `AWS_REGION`、次に`AWS_DEFAULT_REGION` | リクエストの署名に使うリージョン。AWSでは送り先のリージョンでもあります。指定がなければ、S3では`us-east-1`、SQSではキューのURLに含まれるリージョンを使います |
| `endpoint` | String | No | AWS | AWS互換のサービスの場所。`http://localhost:9000`など |
| `access_key_id` | String | No | `AWS_ACCESS_KEY_ID` | アクセスキー |
| `secret_access_key` | String | No | `AWS_SECRET_ACCESS_KEY` | シークレットアクセスキー |
| `session_token` | String | No | `AWS_SESSION_TOKEN` | 一時的な認証情報のセッショントークン |
| `timeout` | Duration | No | `30s` | ステップ全体の制限時間。`10s`または秒数で指定します。`0`で制限なしになります |

これら以外のキーを`with`に書くと、何も送らずにステップが失敗します。`action.yml`がこれらを`params`として宣言しているので、`probe check`はそのようなキーを行番号つきで報告します。

ジョブの`defaults`を、ワークフローがアクションに付けた名前をキーにして書くと、`s3`などの中身も含めてキーごとにマージします。そのため、リージョン、エンドポイント、バケットを各ステップに書かずに済みます。

```yaml
defaults:
  aws:
    endpoint: http://localhost:9000
    s3:
      bucket: fixtures
```

### 認証情報

`with`に`access_key_id`、`secret_access_key`、`session_token`のどれもなければ、アクションはProbeを実行している環境から`AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`、`AWS_SESSION_TOKEN`を読みます。アクションはプロファイルもインスタンスメタデータも、そのほかの認証情報の取得元も読まないので、サービス以外には接続しません。プロファイルやロールの認証情報は、`eval "$(aws configure export-credentials --format env)"`のようにして、Probeを実行する前に環境変数としてエクスポートしてください。

どちらにも認証情報がなければ、S3のリクエストは公開バケットが受け付ける形で、署名なしで送ります。`sqs`と`request`はステップが失敗します。

できるだけ、認証情報は環境変数で渡してください。`with`に書いたシークレットアクセスキーは、結果にもレポートにも、アクション自身のログにも出ません。また、Probeはステップのパラメータを表示するすべての箇所で、`secret_access_key`と`session_token`の値を`<redacted>`と表示します。その変数を[`secrets`](/ja/reference/yaml-configuration#secrets)に書けば、`echo`のように、それ以外の箇所に現れる値も伏せられます。

## S3

`s3`は次のキーのどれか1つで操作を指定します。書けるのは1つだけです。

| キー | 値 | 動作 |
|---|---|---|
| `get` | オブジェクトのキー | オブジェクトを読みます。内容、ダイジェスト、ヘッダーが示す情報を返します |
| `head` | オブジェクトのキー | 内容を読まずに、ヘッダーが示すオブジェクトの情報だけを読みます |
| `list` | プレフィックス、または空 | キーがプレフィックスで始まるオブジェクト、またはすべてのオブジェクトを一覧します |
| `put` | オブジェクトのキー | `body`または`file`からオブジェクトを書きます |
| `delete` | オブジェクトのキー | オブジェクトを削除します |

残りのパラメータは、バケットの場所と書き込む内容を指定します。

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `bucket` | String | Yes | - | バケット |
| `path_style` | Boolean | No | 下記参照 | バケットを、`<bucket>.<endpoint>/<key>`のようにホストではなく、`<endpoint>/<bucket>/<key>`のようにパスで指定します |
| `body` | String、またはほかの値 | `put`で`file`がないとき | - | `put`が書く内容。文字列はそのまま、それ以外はJSONとして送ります。JSONの場合、`content_type`の指定がなければコンテンツタイプは`application/json`です |
| `file` | String | `put`で`body`がないとき | - | `put`が書くファイルのパス。作業ディレクトリからの相対パスです |
| `content_type` | String | No | - | `put`がオブジェクトと一緒に保存するメディアタイプ |
| `metadata` | Object | No | - | `put`がオブジェクトと一緒に保存するメタデータ。`x-amz-meta-<name>`として送ります。名前は小文字にし、値は表示可能なASCIIである必要があります |
| `max_keys` | Integer | No | ストレージの既定値。S3では`1000` | `list`が返すオブジェクトの最大数 |

これら以外のキーを`s3`に書くと、何も送らずにステップが失敗します。`get`に`body`を付けるように、操作が受け付けないパラメータを指定した場合も同じように失敗します。

`path_style`のデフォルトは、`endpoint`があれば`true`です。MinIOなどは1つの名前で提供されるためです。AWSでは`false`ですが、名前にドットを含むバケットは例外です。`*.s3.<region>.amazonaws.com`の証明書がその名前に対応しないためです。

`list`が返すのは1ページ分です。最大`max_keys`個のオブジェクトを返し、ストレージにまだ残りがあるかは`res.truncated`で分かります。マルチパートアップロードには対応していません。

### レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | HTTPステータスコード。`200`など。`delete`では`204`です |
| `res.status` | String | HTTPステータス行。`"200 OK"`など |
| `res.headers` | Object | レスポンスヘッダー。`Content-Type`のような正規化した名前がキーです |
| `res.error_code` | String | ストレージが返したエラー。`NoSuchKey`、`NoSuchBucket`、`AccessDenied`、`SignatureDoesNotMatch`など。なければ空です。`head`の応答にはエラーを含む本文がないので、常に空です |
| `res.error_message` | String | ストレージによるエラーの説明。なければ空です |
| `req` | Object | 実行した内容。`operation`、`bucket`、`region`、`endpoint`、`path_style`、応答を返した`url`、`signed`と、オブジェクトの`key`、または一覧の`prefix`と`max_keys`です。`put`ではさらに`body`、`file`、`content_type`、`metadata`と、送った内容の`size`と`sha256`があります。認証情報は含みません |
| `rt` | Duration | ステップにかかった時間 |
| `status` | Integer | ステータスコードが2xxなら`0`、そうでなければ`1` |

`get`と`head`では、ヘッダーが示すオブジェクトの情報が加わります。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.etag` | String | ETag。引用符は除きます |
| `res.size` | Integer | バイト単位のサイズ |
| `res.content_type` | String | オブジェクトと一緒に保存されているメディアタイプ |
| `res.last_modified` | String | 最後に書かれた日時。`2026-10-10T01:02:03Z`の形式です |
| `res.metadata` | Object | オブジェクトと一緒に保存されているメタデータ。`x-amz-meta-`を除いた小文字の名前がキーです |

`get`では内容が加わります。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.body` | Any | テキストとしての内容。`res.content_type`がJSONを示し、内容がJSONであれば、オブジェクトまたは配列に解析します。バイナリのオブジェクトでは空です |
| `res.rawbody` | String | 解析する前の内容。JSONとして解析したときにだけあります |
| `res.sha256` | String | オブジェクト全体のSHA-256ダイジェスト。16進数です |
| `res.truncated` | Boolean | オブジェクトが1 MiBより大きいかどうか。大きい場合、`res.body`には先頭の1 MiBが入ります |
| `res.binary` | Boolean | 内容がUTF-8のテキストでなく、`res.body`に入っていないかどうか |

`res.sha256`と`res.size`は、オブジェクトがどれだけ大きくても全体についての値です。バイナリや大きなオブジェクトは、書き込んだステップの`req.sha256`や既知のダイジェストとこれらを比べて確かめます。

`put`では`res.etag`が加わります。`list`では次のものが加わります。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.objects` | Array | オブジェクト。キーの順に並び、それぞれ`key`、`size`、`etag`、`last_modified`を持ちます |
| `res.count` | Integer | `res.objects`にあるオブジェクトの数 |
| `res.truncated` | Boolean | ストレージに、返した分より多くのオブジェクトがあるかどうか |

ストレージが応答した後は、その応答が結果になります。そのため、消えているはずのキーの`404`や、アクセスを拒否するはずのバケットの`403`をテストで確かめられます。エラーとして失敗するのは、接続の拒否やタイムアウトのように、応答が得られなかったリクエストだけです。

### リダイレクト

AWSでは、リクエストに署名したリージョンにないバケットを、AWSが応答で示したリージョンから読みます。ストレージが返した`Location`も、行き先がストレージのホストである場合は5回まで追います。`amazonaws.com`の下、または`endpoint`のホストとその下の名前がこれにあたります。リクエストは行き先ごとに署名し直します。それ以外へのリダイレクトは追わず、そのまま結果になります。

## SQS

`sqs`は次のキーのどれか1つで操作を指定します。キーの値は、`https://sqs.us-east-1.amazonaws.com/123456789012/jobs`のようなキューのURLです。

| キー | 動作 | 受け付けるパラメータ |
|---|---|---|
| `receive` | メッセージを受信します。メッセージが届くまで最大`wait`だけ待ちます | `wait`、`max`、`visibility_timeout`、`delete_received` |
| `send` | メッセージを送信します | `body`、`delay`、`group_id`、`deduplication_id`、`attributes` |
| `delete` | 以前に受信したメッセージを削除します | `receipt_handle` |

| パラメータ | 型 | デフォルト | 説明 |
|---|---|---|---|
| `wait` | Duration | `0s` | `receive`がメッセージの到着を待つ時間。最大`20s`で、ロングポーリングになります |
| `max` | Integer | `1` | `receive`が返すメッセージの最大数。最大`10`です |
| `visibility_timeout` | Duration | キューの設定値 | 受信したメッセージをほかのコンシューマーから隠す時間。最大`12h`です。`0`ならメッセージをキューに残したまま中身を確かめられます |
| `delete_received` | Boolean | `false` | `receive`が返したメッセージを、受け取った後にそれぞれ削除します |
| `body` | String、またはほかの値 | - | `send`が送る内容。文字列はそのまま、それ以外はJSONとして送ります |
| `delay` | Duration | キューの設定値 | メッセージを受信できるようになるまでの待ち時間。最大`15m`です |
| `group_id`、`deduplication_id` | String | - | FIFOキューに送るメッセージのグループIDと重複排除ID |
| `attributes` | Object | - | メッセージ属性。それぞれ`String`として送ります |
| `receipt_handle` | String | - | `delete`が削除するメッセージの受信ハンドル。`res.messages[].receipt_handle`から得ます |

時間は`20s`または秒数で書きます。SQSの仕様に合わせて、秒単位の整数である必要があります。AWSでは、`region`で別のリージョンを指定しない限り、キューのURLに含まれるリージョンを使います。`endpoint`があればリクエストはそこに送り、リージョンは`region`か環境変数で指定する必要があります。

ステップは`wait`の時間だけメッセージを待ち、`retry`はメッセージが届くまでそれを繰り返します。

```yaml
- name: Wait for a job
  uses: aws
  with:
    sqs:
      receive: "{{vars.queue_url}}"
      wait: 20s
      delete_received: true
  test: res.count > 0 && res.messages[0].body.status == "done"
  retry:
    max_attempts: 180   # an hour
```

### レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | HTTPステータスコード。成功なら`200`です |
| `res.status` | String | HTTPステータス行 |
| `res.headers` | Object | レスポンスヘッダー |
| `res.error_code` | String | キューが返したエラー。`QueueDoesNotExist`、`AccessDenied`など。なければ空です |
| `res.error_message` | String | キューによるエラーの説明。なければ空です |
| `req` | Object | 実行した内容。`operation`、`queue_url`、`region`、送り先の`url`、`signed`と、操作に渡した値です。認証情報は含みません |
| `rt` | Duration | ステップにかかった時間 |
| `status` | Integer | ステータスコードが2xxなら`0`、そうでなければ`1` |

`receive`では次のものが加わります。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.messages` | Array | 受信したメッセージ。`wait`の間に届かなければ空です |
| `res.messages[].id` | String | メッセージID |
| `res.messages[].body` | Any | 本文。JSONであれば、オブジェクトまたは配列に解析します |
| `res.messages[].rawbody` | String | 解析する前の本文。JSONとして解析したときにだけあります |
| `res.messages[].receipt_handle` | String | このメッセージを`delete`するときに渡す値 |
| `res.messages[].md5` | String | 本文のMD5ダイジェスト |
| `res.messages[].attributes` | Object | メッセージ属性。名前がキーです |
| `res.messages[].system` | Object | SQSが管理する属性。`SentTimestamp`、`ApproximateReceiveCount`など |
| `res.count` | Integer | `res.messages`にあるメッセージの数 |
| `res.deleted` | Integer | `delete_received`で削除したメッセージの数 |

`send`では`res.message_id`、`res.md5`と、FIFOキューでは`res.sequence_number`が加わります。

## Request

`request`は自分で書いたリクエストを、指定したサービス向けに署名して送ります。これにより、HTTPで送るAWSの任意のAPIを呼び出せます。

```yaml
- name: The queue has no messages left
  uses: aws
  with:
    region: ap-northeast-1
    request:
      service: sqs
      post: https://sqs.ap-northeast-1.amazonaws.com/
      headers:
        content-type: application/x-amz-json-1.0
        x-amz-target: AmazonSQS.GetQueueAttributes
      body:
        QueueUrl: "{{vars.queue_url}}"
        AttributeNames: [ApproximateNumberOfMessages]
  test: res.code == 200 && res.body.Attributes.ApproximateNumberOfMessages == "0"
```

| パラメータ | 型 | 必須 | 説明 |
|---|---|---|---|
| `service` | String | Yes | エンドポイントが署名に使うサービス名。`sqs`、API Gatewayの`execute-api`、関数URLの`lambda`、OpenSearchの`es`など |
| `get`、`head`、`post`、`put`、`patch`、`delete`、`options` | String | どれか1つ、または`method`と`url` | そのメソッドで送るURL。`/`で始まるパスは`endpoint`の下のパスとして扱います |
| `method`、`url` | String | | メソッドとURL。上のキーの代わりに使います |
| `headers` | Object | No | リクエストヘッダー。署名を送るのに使うため、`authorization`は指定できません |
| `body` | String、またはほかの値 | No | 本文。文字列はそのまま、それ以外はJSONとして送ります。JSONの場合、`headers`の指定がなければコンテンツタイプは`application/json`です |

リージョンは`region`か環境変数で指定する必要があります。署名はメソッド、URL、本文、ヘッダーを対象にしますが、`User-Agent`、`Accept-Encoding`、`Expect`、`X-Amzn-Trace-Id`は含みません。リダイレクトは追わず、そのまま結果になります。

結果には`res.code`、`res.status`、`res.headers`、`res.body`があります。`res.body`は、コンテンツタイプにかかわらずJSONであれば解析し、解析する前の本文は`res.rawbody`に入ります。本文がUTF-8のテキストでないことは`res.binary`で分かり、その場合`res.body`には入りません。`req`には送った内容が認証情報を除いて入り、`status`は2xxなら`0`です。

サービスが応答した後は、その応答が結果になります。そのため、消えているはずのものの`404`や、アクセスを拒否するはずのものの`403`をテストで確かめられます。エラーとして失敗するのは、接続の拒否やタイムアウトのように、応答が得られなかったリクエストだけです。

## ガードの下での動作

このアクションは実行のガードに従い、`action.yml`で`guard: [read-only, allow-host]`を宣言しています。そのため、`--read-only`と`--allow-host`の下でも`--allow-action`なしで実行されます。

- `--read-only`の下では、何も変更しないものだけを実行します。`s3`の`get`、`head`、`list`、メッセージをキューに残す`visibility_timeout: 0`で`delete_received`のない`sqs`の`receive`、そして`GET`、`HEAD`、`OPTIONS`の`request`です。それ以外は何も送らずに拒否します。
- `--allow-host`の下では、S3のリダイレクト先も含めて、リクエストの送り先のホストがすべて、実行で許可されている必要があります。ポートのないURLは、スキームの既定のポート（`http`は`80`、`https`は`443`）として扱います。`--allow-host '*.amazonaws.com'`とすれば、どのリージョンのAWSも許可されます。

拒否されたステップは、種別`refused`で失敗します。[ガード](/ja/guide/concepts/guard)を参照してください。

## probe-s3からの移行

probe-awsは[probe-s3](https://github.com/mozership/probe-s3)に代わるものです。probe-s3のステップは、操作とバケットのキーを`s3`の下に移し、リージョン、エンドポイント、認証情報、タイムアウトは元の場所のまま残します。

```yaml
# probe-s3
uses: s3
with:
  region: ap-northeast-1
  bucket: example-avatars
  head: avatars/42.png
```

```yaml
# probe-aws
uses: aws
with:
  region: ap-northeast-1
  s3:
    bucket: example-avatars
    head: avatars/42.png
```

`s3`のステップの結果は、probe-s3と同じです。

## 関連項目

- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
- **[HTTP](/ja/reference/actions/http)** - 通常のHTTPリクエスト
- **[Redis](/ja/reference/actions/redis)** - RedisまたはValkeyのサーバーでのコマンド
