package blobs

// NewS3Plain is NewS3 over plain http, for the in-process fake.
func NewS3Plain(endpoint, bucket, accessKey, secretKey, region string) (*S3, error) {
	return newS3(endpoint, bucket, accessKey, secretKey, region, false)
}
