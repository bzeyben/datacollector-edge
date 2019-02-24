// +build aws

// Copyright 2019 StreamSets Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package s3

import (
	"bytes"
	"compress/gzip"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/sirupsen/logrus"
	"github.com/streamsets/datacollector-edge/api"
	"github.com/streamsets/datacollector-edge/api/dataformats"
	"github.com/streamsets/datacollector-edge/container/util"
	"io"
	"strconv"
	"time"
)

type DefaultFileHelper struct {
	filecount            int
	stageContext         api.StageContext
	dataGeneratorService dataformats.DataFormatGeneratorService
	uploader             *s3manager.Uploader
	s3TargetConfigBean   TargetConfigBean
}

func (f *DefaultFileHelper) Handle(
	records []api.Record,
	bucket string,
	keyPrefix string,
) (*s3manager.UploadOutput, error) {
	batchBuffer := bytes.NewBuffer([]byte{})

	var writer io.Writer
	if f.s3TargetConfigBean.Compress {
		writer = gzip.NewWriter(batchBuffer)
	} else {
		writer = batchBuffer
	}

	recordWriter, err := f.dataGeneratorService.GetGenerator(writer)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		err = recordWriter.WriteRecord(record)
		if err != nil {
			logrus.Error(err.Error())
			f.stageContext.ToError(err, record)
		}
	}
	f.flushAndCloseWriter(recordWriter)

	keyPrefix += strconv.FormatInt(util.ConvertTimeToLong(time.Now()), 10) + "-"
	fileName := f.getUniqueDateWithIncrementalFileName(keyPrefix)

	return f.uploader.Upload(f.getUploadInput(bucket, fileName, batchBuffer))
}

func (f *DefaultFileHelper) getUniqueDateWithIncrementalFileName(keyPrefix string) string {
	f.filecount++
	fileName := keyPrefix + strconv.Itoa(f.filecount)

	if len(f.s3TargetConfigBean.FileNameSuffix) > 0 {
		fileName += "." + f.s3TargetConfigBean.FileNameSuffix
	}

	if f.s3TargetConfigBean.Compress {
		fileName += GzipExtension
	}

	return fileName
}

func (f *DefaultFileHelper) flushAndCloseWriter(recordWriter dataformats.RecordWriter) {
	err := recordWriter.Flush()
	if err != nil {
		logrus.WithError(err).Error("Error flushing record writer")
	}

	err = recordWriter.Close()
	if err != nil {
		logrus.WithError(err).Error("Error closing record writer")
	}
}

func (f *DefaultFileHelper) getUploadInput(
	bucket string,
	fileName string,
	body io.Reader,
) *s3manager.UploadInput {
	uploadInput := &s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fileName),
		Body:   body,
	}

	if f.s3TargetConfigBean.SseConfig.UseSSE {
		switch f.s3TargetConfigBean.SseConfig.Encryption {
		case SseS3:
			uploadInput.ServerSideEncryption = aws.String(AES256)
		case SseKMS:
			uploadInput.ServerSideEncryption = aws.String(KMS)
			uploadInput.SSEKMSKeyId = aws.String(f.s3TargetConfigBean.SseConfig.KmsKeyId)
		case SseCustomer:
			uploadInput.SSECustomerAlgorithm = aws.String(AES256)
			uploadInput.SSECustomerKey = aws.String(f.s3TargetConfigBean.SseConfig.CustomerKey)
			uploadInput.SSECustomerKeyMD5 = aws.String(f.s3TargetConfigBean.SseConfig.CustomerKeyMd5)
		}
	}

	return uploadInput
}
