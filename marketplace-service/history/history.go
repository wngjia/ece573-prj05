// Package history provides Cassandra-backed marketplace read views.
package history

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// History provides marketplace read views backed by Cassandra.
type History struct {
	session           *gocql.Session
	replicationFactor int
	readConsistency   gocql.Consistency
	writeConsistency  gocql.Consistency
	writeRetries      int
	retryInterval     time.Duration
}

// DatabaseConfig contains the Cassandra connection settings.
type DatabaseConfig struct {
	ContactPoints     string
	ReplicationFactor int
	ReadConsistency   string
	WriteConsistency  string
	WriteRetries      int
	RetryInterval     time.Duration
}

// OpenHistory connects to Cassandra and initializes the history schema.
func OpenHistory(config DatabaseConfig) (*History, error) {
	readConsistency, err := parseConsistency(config.ReadConsistency)
	if err != nil {
		return nil, fmt.Errorf("READ_CONSISTENCY: %w", err)
	}
	writeConsistency, err := parseConsistency(config.WriteConsistency)
	if err != nil {
		return nil, fmt.Errorf("WRITE_CONSISTENCY: %w", err)
	}
	if config.WriteRetries < 0 || config.WriteRetries > 20 {
		return nil, fmt.Errorf("CASSANDRA_WRITE_RETRIES must be between 0 and 20")
	}
	if config.RetryInterval < 0 || config.RetryInterval > 30*time.Second {
		return nil, fmt.Errorf("CASSANDRA_RETRY_INTERVAL must be between 0 and 30s")
	}
	if config.ReplicationFactor < 1 || config.ReplicationFactor > 3 {
		return nil, fmt.Errorf("CASSANDRA_REPLICATION_FACTOR must be between 1 and 3")
	}

	cluster := gocql.NewCluster(strings.Split(config.ContactPoints, ",")...)
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Logger = log.New(os.Stderr, "WARN Cassandra driver: ", log.LstdFlags)

	log.Printf("INFO connecting to Cassandra using contact points %s", config.ContactPoints)
	var session *gocql.Session
	for attempt := 1; ; attempt++ {
		var connectErr error
		session, connectErr = cluster.CreateSession()
		if connectErr == nil {
			break
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("WARN Cassandra startup connection attempt %d failed: %v; retrying in 2s", attempt, connectErr)
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("INFO connected to Cassandra")

	history := &History{session: session, replicationFactor: config.ReplicationFactor,
		readConsistency: readConsistency, writeConsistency: writeConsistency,
		writeRetries: config.WriteRetries, retryInterval: config.RetryInterval}
	for attempt := 1; ; attempt++ {
		if err := history.createSchema(); err == nil {
			log.Printf("INFO Cassandra history schema ready")
			return history, nil
		} else {
			if attempt == 1 || attempt%10 == 0 {
				log.Printf("WARN Cassandra startup schema attempt %d failed: %v; retrying in 2s", attempt, err)
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func parseConsistency(value string) (gocql.Consistency, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ONE":
		return gocql.One, nil
	case "QUORUM":
		return gocql.Quorum, nil
	case "ALL":
		return gocql.All, nil
	default:
		return 0, fmt.Errorf("must be ONE, QUORUM, or ALL")
	}
}

// Close releases the Cassandra session.
func (s *History) Close() {
	s.session.Close()
}

func (s *History) createSchema() error {
	keyspace := fmt.Sprintf(`CREATE KEYSPACE IF NOT EXISTS marketplace_history
		WITH replication = {'class': 'NetworkTopologyStrategy', 'datacenter1': %d}`, s.replicationFactor)
	if err := s.session.Query(keyspace).Exec(); err != nil {
		return fmt.Errorf("create history keyspace: %w", err)
	}

	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS marketplace_history.purchases_by_buyer (
			buyer_id TEXT, order_day TEXT, ordered_at TIMESTAMP, order_id TEXT,
			listing_id TEXT, seller_id TEXT, title TEXT, credit_cost INT, status TEXT,
			listed_at TIMESTAMP, processed_at TIMESTAMP,
			PRIMARY KEY ((buyer_id), order_day, ordered_at, order_id)
		) WITH CLUSTERING ORDER BY (order_day ASC, ordered_at ASC, order_id ASC)`,
		`CREATE TABLE IF NOT EXISTS marketplace_history.sales_by_seller (
			seller_id TEXT, order_day TEXT, ordered_at TIMESTAMP, order_id TEXT,
			listing_id TEXT, buyer_id TEXT, title TEXT, credit_cost INT, status TEXT,
			listed_at TIMESTAMP, processed_at TIMESTAMP,
			PRIMARY KEY ((seller_id), order_day, ordered_at, order_id)
		) WITH CLUSTERING ORDER BY (order_day ASC, ordered_at ASC, order_id ASC)`,
		`CREATE TABLE IF NOT EXISTS marketplace_history.listings_by_seller (
			seller_id TEXT, listed_at TIMESTAMP, listing_id TEXT,
			title TEXT, credit_cost INT, status TEXT,
			PRIMARY KEY ((seller_id), listed_at, listing_id)
		) WITH CLUSTERING ORDER BY (listed_at ASC, listing_id ASC)`,
		`CREATE TABLE IF NOT EXISTS marketplace_history.available_listings (
			feed TEXT, listed_at TIMESTAMP, listing_id TEXT,
			seller_id TEXT, title TEXT, credit_cost INT, status TEXT,
			PRIMARY KEY ((feed), listed_at, listing_id)
		) WITH CLUSTERING ORDER BY (listed_at ASC, listing_id ASC)`,
	} {
		if err := s.session.Query(statement).Exec(); err != nil {
			return fmt.Errorf("create history table: %w", err)
		}
	}
	return nil
}

func (s *History) write(statement string, values ...any) error {
	// TODO Project 5: retry transient write errors using transientCassandraError.
	// Allow s.writeRetries additional attempts, wait s.retryInterval between
	// attempts (hint: time.Sleep(s.retryInterval)), and return the final error
	// if all attempts fail.
	return s.session.Query(statement, values...).Consistency(s.writeConsistency).Exec()
}

func transientCassandraError(err error) bool {
	for _, candidate := range []error{
		gocql.ErrTimeoutNoResponse, gocql.ErrTooManyTimeouts,
		gocql.ErrConnectionClosed, gocql.ErrNoConnections, gocql.ErrNoStreams,
		gocql.ErrUnavailable,
	} {
		if errors.Is(err, candidate) {
			return true
		}
	}
	var requestErr gocql.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.Code() {
		case gocql.ErrCodeUnavailable, gocql.ErrCodeOverloaded,
			gocql.ErrCodeBootstrapping, gocql.ErrCodeWriteTimeout:
			return true
		}
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
