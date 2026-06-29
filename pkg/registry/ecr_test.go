package registry

import (
    "context"
    "fmt"
    "sort"
    "testing"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/ecr"
    "github.com/aws/aws-sdk-go-v2/service/ecr/types"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
)