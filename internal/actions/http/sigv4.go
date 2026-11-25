package http

import (
	"cmp"
	"errors"
	"fmt"
	hp "net/http"
	"slices"
	"time"

	"github.com/mozership/probe/internal/sigv4"
)

const sigV4Key = "aws_sigv4"

var sigV4Keys = []string{"service", "region", "access_key_id", "secret_access_key", "session_token"}

// awsSigning is what a request is signed with when the step gives
// aws_sigv4.
type awsSigning struct {
	creds   sigv4.Credentials
	region  string
	service string
}

func (s *awsSigning) sign(req *hp.Request, body []byte) {
	sigv4.Sign(req, s.creds, s.region, s.service, sigv4.PayloadHash(body), time.Now())
}

// takeSigV4 takes aws_sigv4 out of m and returns how to sign the request,
// or nil when the step does not ask for a signature. The region and the
// credentials not given fall back to the environment, as the AWS CLI reads
// them.
func takeSigV4(m map[string]any, getenv func(string) string) (*awsSigning, error) {
	v, ok := m[sigV4Key]
	if !ok {
		return nil, nil
	}
	delete(m, sigV4Key)
	given, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("aws_sigv4 must be a map of service, region and the credentials")
	}
	fields := map[string]string{}
	for k, v := range given {
		if !slices.Contains(sigV4Keys, k) {
			return nil, fmt.Errorf("aws_sigv4 takes service, region, access_key_id, secret_access_key and session_token, not %s", k)
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("aws_sigv4.%s must be a string", k)
		}
		fields[k] = s
	}

	s := &awsSigning{service: fields["service"]}
	if s.service == "" {
		return nil, errors.New("aws_sigv4.service is required: the name of the service in its endpoint, such as sqs or execute-api")
	}
	s.region = cmp.Or(fields["region"], getenv("AWS_REGION"), getenv("AWS_DEFAULT_REGION"))
	if s.region == "" {
		return nil, errors.New("aws_sigv4.region is required when neither AWS_REGION nor AWS_DEFAULT_REGION is set")
	}

	s.creds = sigv4.Credentials{
		AccessKeyID:     fields["access_key_id"],
		SecretAccessKey: fields["secret_access_key"],
		SessionToken:    fields["session_token"],
	}
	switch {
	case (s.creds.AccessKeyID == "") != (s.creds.SecretAccessKey == ""):
		return nil, errors.New("aws_sigv4.access_key_id and aws_sigv4.secret_access_key must be given together")
	case s.creds.AccessKeyID == "" && s.creds.SessionToken != "":
		return nil, errors.New("aws_sigv4.session_token needs aws_sigv4.access_key_id and aws_sigv4.secret_access_key")
	case s.creds.AccessKeyID == "":
		creds, err := sigv4.FromEnv(getenv)
		if err != nil {
			return nil, err
		}
		if creds.AccessKeyID == "" {
			return nil, errors.New("aws_sigv4 needs credentials: give aws_sigv4.access_key_id and aws_sigv4.secret_access_key, or set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
		}
		s.creds = creds
	}
	return s, nil
}
