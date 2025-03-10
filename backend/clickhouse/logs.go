package clickhouse

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/highlight-run/highlight/backend/queryparser"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	e "github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/google/uuid"
	modelInputs "github.com/highlight-run/highlight/backend/private-graph/graph/model"
	"github.com/highlight-run/highlight/backend/util"
	"github.com/huandu/go-sqlbuilder"
	"github.com/samber/lo"
)

const LogsTable = "logs"
const LogsSamplingTable = "logs_sampling"
const LogKeysTable = "log_keys"
const LogKeyValuesTable = "log_key_values"

var logKeysToColumns = map[modelInputs.ReservedLogKey]string{
	modelInputs.ReservedLogKeyLevel:           "SeverityText",
	modelInputs.ReservedLogKeySecureSessionID: "SecureSessionId",
	modelInputs.ReservedLogKeySpanID:          "SpanId",
	modelInputs.ReservedLogKeyTraceID:         "TraceId",
	modelInputs.ReservedLogKeySource:          "Source",
	modelInputs.ReservedLogKeyServiceName:     "ServiceName",
	modelInputs.ReservedLogKeyServiceVersion:  "ServiceVersion",
}

var logsTableConfig = tableConfig[modelInputs.ReservedLogKey]{
	tableName:        LogsTable,
	keysToColumns:    logKeysToColumns,
	reservedKeys:     modelInputs.AllReservedLogKey,
	bodyColumn:       "Body",
	attributesColumn: "LogAttributes",
	selectColumns: []string{
		"Timestamp",
		"UUID",
		"SeverityText",
		"Body",
		"LogAttributes",
		"TraceId",
		"SpanId",
		"SecureSessionId",
		"Source",
		"ServiceName",
		"ServiceVersion",
	},
}

var logsSamplingTableConfig = tableConfig[modelInputs.ReservedLogKey]{
	tableName:        fmt.Sprintf("%s SAMPLE %d", LogsSamplingTable, SamplingRows),
	keysToColumns:    logKeysToColumns,
	reservedKeys:     modelInputs.AllReservedLogKey,
	bodyColumn:       "Body",
	attributesColumn: "LogAttributes",
}

func (client *Client) BatchWriteLogRows(ctx context.Context, logRows []*LogRow) error {
	if len(logRows) == 0 {
		return nil
	}

	rows := lo.Map(logRows, func(l *LogRow, _ int) interface{} {
		if len(l.UUID) == 0 {
			l.UUID = uuid.New().String()
		}
		return l
	})

	batch, err := client.conn.PrepareBatch(ctx, fmt.Sprintf("INSERT INTO %s", LogsTable))
	if err != nil {
		return e.Wrap(err, "failed to create logs batch")
	}

	for _, logRow := range rows {
		err = batch.AppendStruct(logRow)
		if err != nil {
			return err
		}
	}

	return batch.Send()
}

const LogsLimit int = 50
const KeyValuesLimit int = 50

const OrderBackwardNatural = "Timestamp ASC, UUID ASC"
const OrderForwardNatural = "Timestamp DESC, UUID DESC"

const OrderBackwardInverted = "Timestamp DESC, UUID DESC"
const OrderForwardInverted = "Timestamp ASC, UUID ASC"

type Pagination struct {
	After     *string
	Before    *string
	At        *string
	Direction modelInputs.SortDirection
	CountOnly bool
}

func (client *Client) ReadLogs(ctx context.Context, projectID int, params modelInputs.QueryInput, pagination Pagination) (*modelInputs.LogConnection, error) {
	scanLog := func(rows driver.Rows) (*Edge[modelInputs.Log], error) {
		var result struct {
			Timestamp       time.Time
			UUID            string
			SeverityText    string
			Body            string
			LogAttributes   map[string]string
			TraceId         string
			SpanId          string
			SecureSessionId string
			Source          string
			ServiceName     string
			ServiceVersion  string
		}
		if err := rows.ScanStruct(&result); err != nil {
			return nil, err
		}

		return &Edge[modelInputs.Log]{
			Cursor: encodeCursor(result.Timestamp, result.UUID),
			Node: &modelInputs.Log{
				Timestamp:       result.Timestamp,
				Level:           makeLogLevel(result.SeverityText),
				Message:         result.Body,
				LogAttributes:   expandJSON(result.LogAttributes),
				TraceID:         &result.TraceId,
				SpanID:          &result.SpanId,
				SecureSessionID: &result.SecureSessionId,
				Source:          &result.Source,
				ServiceName:     &result.ServiceName,
				ServiceVersion:  &result.ServiceVersion,
			},
		}, nil
	}

	conn, err := readObjects(ctx, client, logsTableConfig, projectID, params, pagination, scanLog)
	if err != nil {
		return nil, err
	}

	mappedEdges := []*modelInputs.LogEdge{}
	for _, edge := range conn.Edges {
		mappedEdges = append(mappedEdges, &modelInputs.LogEdge{
			Cursor: edge.Cursor,
			Node:   edge.Node,
		})
	}

	return &modelInputs.LogConnection{
		Edges:    mappedEdges,
		PageInfo: conn.PageInfo,
	}, nil
}

// This is a lighter weight version of the previous function for loading the minimal about of data for a session
func (client *Client) ReadSessionLogs(ctx context.Context, projectID int, params modelInputs.QueryInput) ([]*modelInputs.LogEdge, error) {
	selectStr := "Timestamp, UUID, SeverityText, Body"

	sb, err := makeSelectBuilder(
		logsTableConfig,
		selectStr,
		nil,
		projectID,
		params,
		Pagination{},
		OrderBackwardInverted,
		OrderForwardInverted)
	if err != nil {
		return nil, err
	}

	sql, args := sb.BuildWithFlavor(sqlbuilder.ClickHouse)

	span, _ := util.StartSpanFromContext(ctx, "logs", util.ResourceName("ReadSessionLogs"))
	query, err := sqlbuilder.ClickHouse.Interpolate(sql, args)
	if err != nil {
		span.Finish(err)
		return nil, err
	}
	span.SetAttribute("Query", query)
	span.SetAttribute("Params", params)

	rows, err := client.conn.Query(ctx, sql, args...)

	if err != nil {
		span.Finish(err)
		return nil, err
	}

	edges := []*modelInputs.LogEdge{}

	for rows.Next() {
		var result struct {
			Timestamp    time.Time
			UUID         string
			SeverityText string
			Body         string
		}
		if err := rows.ScanStruct(&result); err != nil {
			return nil, err
		}

		edges = append(edges, &modelInputs.LogEdge{
			Cursor: encodeCursor(result.Timestamp, result.UUID),
			Node: &modelInputs.Log{
				Timestamp: result.Timestamp,
				Level:     makeLogLevel(result.SeverityText),
				Message:   result.Body,
			},
		})
	}
	rows.Close()
	span.Finish(rows.Err())
	return edges, rows.Err()
}

func (client *Client) ReadLogsTotalCount(ctx context.Context, projectID int, params modelInputs.QueryInput) (uint64, error) {
	sb, err := makeSelectBuilder(
		logsTableConfig,
		"COUNT(*)",
		nil,
		projectID,
		params,
		Pagination{CountOnly: true},
		OrderBackwardNatural,
		OrderForwardNatural)
	if err != nil {
		return 0, err
	}

	sql, args := sb.BuildWithFlavor(sqlbuilder.ClickHouse)

	var count uint64
	err = client.conn.QueryRow(
		ctx,
		sql,
		args...,
	).Scan(&count)

	return count, err
}

type number interface {
	uint64 | float64
}

func (client *Client) ReadTracesDailySum(ctx context.Context, projectIds []int, dateRange modelInputs.DateRangeRequiredInput) (uint64, error) {
	return readDailyImpl[uint64](ctx, client, "trace_count_daily_mv", "sum", projectIds, dateRange)
}

func (client *Client) ReadTracesDailyAverage(ctx context.Context, projectIds []int, dateRange modelInputs.DateRangeRequiredInput) (float64, error) {
	return readDailyImpl[float64](ctx, client, "trace_count_daily_mv", "avg", projectIds, dateRange)
}

func (client *Client) ReadLogsDailySum(ctx context.Context, projectIds []int, dateRange modelInputs.DateRangeRequiredInput) (uint64, error) {
	return readDailyImpl[uint64](ctx, client, "log_count_daily_mv", "sum", projectIds, dateRange)
}

func (client *Client) ReadLogsDailyAverage(ctx context.Context, projectIds []int, dateRange modelInputs.DateRangeRequiredInput) (float64, error) {
	return readDailyImpl[float64](ctx, client, "log_count_daily_mv", "avg", projectIds, dateRange)
}

func readDailyImpl[N number](ctx context.Context, client *Client, table string, aggFn string, projectIds []int, dateRange modelInputs.DateRangeRequiredInput) (N, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(fmt.Sprintf("COALESCE(%s(Count), 0) AS Count", aggFn)).
		From(table).
		Where(sb.In("ProjectId", projectIds)).
		Where(sb.LessThan("toUInt64(Day)", uint64(dateRange.EndDate.Unix()))).
		Where(sb.GreaterEqualThan("toUInt64(Day)", uint64(dateRange.StartDate.Unix())))

	sql, args := sb.BuildWithFlavor(sqlbuilder.ClickHouse)

	var out N
	err := client.conn.QueryRow(
		ctx,
		sql,
		args...,
	).Scan(&out)

	switch v := any(out).(type) {
	case float64:
		if math.IsNaN(v) {
			return 0, err
		}
	}

	return out, err
}

func (client *Client) ReadLogsHistogram(ctx context.Context, projectID int, params modelInputs.QueryInput, nBuckets int) (*modelInputs.LogsHistogram, error) {
	startTimestamp := uint64(params.DateRange.StartDate.Unix())
	endTimestamp := uint64(params.DateRange.EndDate.Unix())

	// If the queried time range is >= 24 hours, query the sampling table.
	// Else, query the logs table directly.
	var fromSb *sqlbuilder.SelectBuilder
	var err error
	if params.DateRange.EndDate.Sub(params.DateRange.StartDate) >= 24*time.Hour {
		fromSb, err = makeSelectBuilder(
			logsSamplingTableConfig,
			fmt.Sprintf(
				"toUInt64(intDiv(%d * (toRelativeSecondNum(Timestamp) - %d), (%d - %d)) * 8 + SeverityNumber), toUInt64(round(count() * any(_sample_factor))), any(_sample_factor)",
				nBuckets,
				startTimestamp,
				endTimestamp,
				startTimestamp,
			),
			nil,
			projectID,
			params,
			Pagination{CountOnly: true},
			OrderBackwardNatural,
			OrderForwardNatural,
		)
	} else {
		fromSb, err = makeSelectBuilder(
			logsTableConfig,
			fmt.Sprintf(
				"toUInt64(intDiv(%d * (toRelativeSecondNum(Timestamp) - %d), (%d - %d)) * 8 + SeverityNumber), count(), 1.0",
				nBuckets,
				startTimestamp,
				endTimestamp,
				startTimestamp,
			),
			nil,
			projectID,
			params,
			Pagination{CountOnly: true},
			OrderBackwardNatural,
			OrderForwardNatural,
		)
	}
	if err != nil {
		return nil, err
	}

	fromSb.GroupBy("1")

	sql, args := fromSb.BuildWithFlavor(sqlbuilder.ClickHouse)

	histogram := &modelInputs.LogsHistogram{
		Buckets:    make([]*modelInputs.LogsHistogramBucket, 0, nBuckets),
		TotalCount: uint64(nBuckets),
	}

	rows, err := client.conn.Query(
		ctx,
		sql,
		args...,
	)

	if err != nil {
		return nil, err
	}

	var (
		groupKey     uint64
		count        uint64
		sampleFactor float64
	)

	buckets := make(map[uint64]map[modelInputs.LogLevel]uint64)

	for rows.Next() {
		if err := rows.Scan(&groupKey, &count, &sampleFactor); err != nil {
			return nil, err
		}

		bucketId := groupKey / 8
		level := logrus.Level(groupKey % 8)

		// clamp bucket to [0, nBuckets)
		if bucketId >= uint64(nBuckets) {
			bucketId = uint64(nBuckets - 1)
		}

		// create bucket if not exists
		if _, ok := buckets[bucketId]; !ok {
			buckets[bucketId] = make(map[modelInputs.LogLevel]uint64)
		}

		// add count to bucket
		buckets[bucketId][getLogLevel(level)] = count
	}

	var objectCount uint64
	for bucketId := uint64(0); bucketId < uint64(nBuckets); bucketId++ {
		if _, ok := buckets[bucketId]; !ok {
			continue
		}
		bucket := buckets[bucketId]
		counts := make([]*modelInputs.LogsHistogramBucketCount, 0, len(bucket))
		for _, level := range modelInputs.AllLogLevel {
			if _, ok := bucket[level]; !ok {
				bucket[level] = 0
			}
			counts = append(counts, &modelInputs.LogsHistogramBucketCount{
				Level: level,
				Count: bucket[level],
			})
			objectCount += bucket[level]
		}

		histogram.Buckets = append(histogram.Buckets, &modelInputs.LogsHistogramBucket{
			BucketID: bucketId,
			Counts:   counts,
		})
	}

	histogram.ObjectCount = objectCount
	histogram.SampleFactor = sampleFactor

	return histogram, err
}

func (client *Client) LogsKeys(ctx context.Context, projectID int, startDate time.Time, endDate time.Time) ([]*modelInputs.QueryKey, error) {
	return KeysAggregated(ctx, client, LogKeysTable, projectID, startDate, endDate)
}

func (client *Client) LogsKeyValues(ctx context.Context, projectID int, keyName string, startDate time.Time, endDate time.Time) ([]string, error) {
	return KeyValuesAggregated(ctx, client, LogKeyValuesTable, projectID, keyName, startDate, endDate)
}

func LogMatchesQuery(logRow *LogRow, filters *queryparser.Filters) bool {
	return matchesQuery(logRow, logsTableConfig, filters)
}
