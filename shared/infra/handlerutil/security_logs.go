package handlerutil

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

var (
	CwClient   *cloudwatchlogs.Client
	CwInitOnce sync.Once
	CwLogGroup string
	CwEnabled  bool
)

// InitCloudWatch initializes the CloudWatch Logs client with the provided log group.
// It logs a warning if logGroup is not configured.
func InitCloudWatch(logGroup string) {
	CwInitOnce.Do(func() {
		CwLogGroup = logGroup
		if CwLogGroup == "" {
			log.Println("security event shipping disabled — CloudWatch log group not set")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Printf("[SECURITY-SHIP-FAILED] failed to load AWS SDK config: %v", err)
			return
		}

		CwClient = cloudwatchlogs.NewFromConfig(cfg)
		CwEnabled = true
		log.Printf("security event shipping enabled: group=%s", CwLogGroup)
	})
}

type SecurityEvent struct {
	Timestamp string `json:"timestamp"`
	EventType string `json:"event_type"`
	Service   string `json:"service"`
	ActorID   string `json:"actor_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
	ClientIP  string `json:"client_ip,omitempty"`
}

// ShipSecurityEvent ships a structured JSON log to CloudWatch.
// It runs asynchronously in a goroutine and does not block.
func ShipSecurityEvent(ctx context.Context, eventType, service, actorID, tenantID, detail, clientIP string) {
	InitCloudWatch("")
	if !CwEnabled || CwClient == nil {
		return
	}

	client := CwClient
	logGroup := CwLogGroup

	// #nosec G118 //nolint:gosec -- async security log shipping must outlive request context to prevent evasion
	go func() {
		shipCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		event := SecurityEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			EventType: eventType,
			Service:   service,
			ActorID:   actorID,
			TenantID:  tenantID,
			Detail:    detail,
			ClientIP:  clientIP,
		}

		payload, err := json.Marshal(event)
		if err != nil {
			log.Printf("[SECURITY-SHIP-FAILED] failed to marshal security event: %v", err)
			return
		}

		logStream := service

		input := &cloudwatchlogs.PutLogEventsInput{
			LogGroupName:  aws.String(logGroup),
			LogStreamName: aws.String(logStream),
			LogEvents: []types.InputLogEvent{
				{
					Message:   aws.String(string(payload)),
					Timestamp: aws.Int64(time.Now().UnixNano() / int64(time.Millisecond)),
				},
			},
		}

		_, err = client.PutLogEvents(shipCtx, input)
		if err != nil {
			var nf *types.ResourceNotFoundException
			if errors.As(err, &nf) {
				_, errCreate := client.CreateLogStream(shipCtx, &cloudwatchlogs.CreateLogStreamInput{
					LogGroupName:  aws.String(logGroup),
					LogStreamName: aws.String(logStream),
				})
				var alreadyExists *types.ResourceAlreadyExistsException
				if errCreate == nil || errors.As(errCreate, &alreadyExists) {
					_, err = client.PutLogEvents(shipCtx, input)
				} else {
					log.Printf("[SECURITY-SHIP-FAILED] failed to create log stream %s: %v", logStream, errCreate)
					return
				}
			}
		}

		if err != nil {
			log.Printf("[SECURITY-SHIP-FAILED] failed to ship to CloudWatch: %v", err)
		}
	}()
}

// GetClientIP extracts client IP address from request.
func GetClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	var ip string
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip = strings.TrimSpace(parts[0])
	} else if rip := r.Header.Get("X-Real-IP"); rip != "" {
		ip = rip
	} else {
		ip = r.RemoteAddr
	}

	if strings.Contains(ip, "]") {
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
		ip = strings.Trim(ip, "[]")
	} else {
		if count := strings.Count(ip, ":"); count == 1 {
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}
		}
	}
	return ip
}
