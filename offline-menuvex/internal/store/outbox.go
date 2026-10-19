package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Operation is immutable wire intent. Sequence, ID and payload survive retries.
type Operation struct {
	ID          string          `json:"operation_id"`
	Sequence    int64           `json:"client_sequence"`
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id"`
	Action      string          `json:"operation"`
	BaseVersion *int64          `json:"base_version,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   string          `json:"client_created_at"`
	State       string          `json:"-"`
	Result      json.RawMessage `json:"-"`
}

type Result struct {
	ID     string          `json:"operation_id"`
	Status string          `json:"status"`
	Error  json.RawMessage `json:"error"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func UUID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}

// The editor intentionally excludes absolute stock, money settlements, order
// transitions and ingredient unit changes. Those require their own workflows.
var catalogFields = map[string]map[string]string{
	"category":   {"title": "text", "slug": "text", "description": "text"},
	"product":    {"title": "text", "slug": "text", "description": "text", "category_id": "uuid", "price": "decimal", "cost": "decimal", "is_available": "bool"},
	"ingredient": {"name": "text", "sku": "text", "barcode": "text", "base_unit": "unit"},
	"banner":     {"title": "text", "image": "text", "link": "text", "is_active": "bool"},
}

var decimalPattern = regexp.MustCompile(`^[0-9]{1,15}(\.[0-9]{1,4})?$`)

func validateIntent(kind, action string, raw json.RawMessage) error {
	fields, ok := catalogFields[kind]
	if !ok {
		return errors.New("this domain has no desktop offline write workflow")
	}
	if action != "create" && action != "update" && action != "delete" {
		return errors.New("unsupported catalog operation")
	}
	if len(raw) > 65536 {
		return errors.New("payload too large")
	}
	var payload map[string]json.RawMessage
	if e := json.Unmarshal(raw, &payload); e != nil || payload == nil {
		return errors.New("payload must be a JSON object")
	}
	if action == "delete" {
		if len(payload) != 0 {
			return errors.New("delete payload must be empty")
		}
		return nil
	}
	if len(payload) == 0 {
		return errors.New("empty edit")
	}
	for key, value := range payload {
		typ, ok := fields[key]
		if !ok {
			return fmt.Errorf("field %q is not editable offline", key)
		}
		if kind == "ingredient" && key == "base_unit" && action != "create" {
			return errors.New("ingredient units cannot be changed offline")
		}
		switch typ {
		case "bool":
			var b bool
			if string(value) == "null" || json.Unmarshal(value, &b) != nil {
				return fmt.Errorf("%s must be a boolean", key)
			}
		case "decimal":
			str := string(value)
			if strings.HasPrefix(str, "\"") {
				if json.Unmarshal(value, &str) != nil {
					return errors.New("invalid decimal")
				}
			}
			if !decimalPattern.MatchString(str) {
				return fmt.Errorf("%s must be a nonnegative exact decimal", key)
			}
		default:
			var str string
			if string(value) == "null" || json.Unmarshal(value, &str) != nil || len([]rune(str)) > 2000 {
				return fmt.Errorf("%s must be text (maximum 2000 characters)", key)
			}
			if (key == "title" || key == "name") && strings.TrimSpace(str) == "" {
				return errors.New("name/title cannot be empty")
			}
			if typ == "uuid" && !uuidPattern.MatchString(str) {
				return fmt.Errorf("%s must be a UUID", key)
			}
			if typ == "unit" && !strings.Contains("|mg|g|kg|ml|cl|l|pcs|pack|portion|bottle|box|shot|sachet|", "|"+str+"|") {
				return errors.New("unknown base unit")
			}
		}
	}
	if action == "create" {
		required := map[string][]string{"category": {"title"}, "product": {"title", "category_id", "price"}, "ingredient": {"name", "base_unit"}, "banner": {"title"}}
		for _, key := range required[kind] {
			if _, ok := payload[key]; !ok {
				return fmt.Errorf("create requires %s", key)
			}
		}
	}
	return nil
}

// QueueCatalog validates and records catalog mutations.
func (s *Store) QueueCatalog(ctx context.Context, kind, id, action string, payload json.RawMessage, expectedVersion int64) (string, error) {
	if e := validateIntent(kind, action, payload); e != nil {
		return "", e
	}
	if action == "create" {
		if id != "" {
			return "", errors.New("new entity IDs are generated locally")
		}
		var e error
		id, e = UUID()
		if e != nil {
			return "", e
		}
	} else if !uuidPattern.MatchString(id) {
		return "", errors.New("entity ID must be a UUID")
	}
	opID, e := UUID()
	if e != nil {
		return "", e
	}
	err := s.withTx(ctx, func(st *persistedDB) error {
		scope := st.Meta["scope"]
		if scope == "" {
			return errors.New("configure a device before queuing server edits")
		}
		for _, op := range st.Outbox {
			if op.EntityType == kind && op.EntityID == id && op.State != "applied" && op.State != "discarded" {
				return errors.New("this record already has pending/blocked intent; resolve it first")
			}
		}
		var base *int64
		if action != "create" {
			existing, ok := st.Entities[entityKey(kind, id)]
			if !ok || existing.Deleted {
				return errors.New("record is not in the current local snapshot")
			}
			if existing.Version != expectedVersion {
				return errors.New("record changed while editing; reopen the editor")
			}
			if existing.Version < 1 {
				return errors.New("record has no optimistic version; refresh before editing")
			}
			v := existing.Version
			base = &v
		}
		rawCopy := make(json.RawMessage, len(payload))
		copy(rawCopy, payload)
		st.NextOutboxSeq++
		st.Outbox = append(st.Outbox, storedOperation{
			ID:          opID,
			Sequence:    st.NextOutboxSeq,
			EntityType:  kind,
			EntityID:    id,
			Action:      action,
			BaseVersion: base,
			Payload:     rawCopy,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			State:       "queued",
			Result:      json.RawMessage(`{}`),
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (op Operation) HasResult() bool {
	s := strings.TrimSpace(string(op.Result))
	return s != "" && s != "{}" && s != "null"
}

func (s *Store) Outbox(ctx context.Context, pending bool) ([]Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ops := []Operation{}
	for _, op := range s.state.Outbox {
		if pending {
			if op.State != "queued" && op.State != "retry" && op.State != "not_processed" {
				continue
			}
		}
		raw := make(json.RawMessage, len(op.Payload))
		copy(raw, op.Payload)
		res := make(json.RawMessage, len(op.Result))
		copy(res, op.Result)
		var bv *int64
		if op.BaseVersion != nil {
			v := *op.BaseVersion
			bv = &v
		}
		ops = append(ops, Operation{
			ID:          op.ID,
			Sequence:    op.Sequence,
			EntityType:  op.EntityType,
			EntityID:    op.EntityID,
			Action:      op.Action,
			BaseVersion: bv,
			Payload:     raw,
			CreatedAt:   op.CreatedAt,
			State:       op.State,
			Result:      res,
		})
	}
	if pending {
		sort.Slice(ops, func(i, j int) bool { return ops[i].Sequence < ops[j].Sequence })
		if len(ops) > 100 {
			ops = ops[:100]
		}
	} else {
		sort.Slice(ops, func(i, j int) bool { return ops[i].Sequence > ops[j].Sequence })
		if len(ops) > 500 {
			ops = ops[:500]
		}
	}
	return ops, nil
}

// SaveResults validates a complete bijection before changing anything. A lost
// or malformed response leaves all original IDs eligible for safe replay.
func (s *Store) SaveResults(ctx context.Context, sent []Operation, results []Result) error {
	if len(sent) != len(results) {
		return errors.New("push response did not cover the submitted batch")
	}
	expected := map[string]bool{}
	for _, op := range sent {
		expected[op.ID] = true
	}
	for _, r := range results {
		if !expected[r.ID] {
			return errors.New("push returned duplicate or unknown operation ID")
		}
		delete(expected, r.ID)
		switch r.Status {
		case "applied", "conflict", "rejected", "retry", "not_processed", "skipped":
		default:
			return errors.New("unknown push result status")
		}
	}
	return s.withTx(ctx, func(st *persistedDB) error {
		byID := map[string]int{}
		for i, op := range st.Outbox {
			byID[op.ID] = i
		}
		for _, r := range results {
			raw, e := json.Marshal(r)
			if e != nil {
				return e
			}
			idx, ok := byID[r.ID]
			if !ok {
				return errors.New("outbox changed during push")
			}
			cur := st.Outbox[idx]
			if cur.State != "queued" && cur.State != "retry" && cur.State != "not_processed" {
				return errors.New("outbox changed during push")
			}
			cur.State = r.Status
			cur.Result = raw
			st.Outbox[idx] = cur
		}
		return nil
	})
}

// Only terminal, known-not-applied intent can be discarded. An uncertain
// network outcome MUST be replayed with its original ID, never cancelled.
func (s *Store) Discard(ctx context.Context, id string) error {
	return s.withTx(ctx, func(st *persistedDB) error {
		for i, op := range st.Outbox {
			if op.ID == id && (op.State == "conflict" || op.State == "rejected" || op.State == "skipped") {
				st.Outbox[i].State = "discarded"
				return nil
			}
		}
		return errors.New("only blocked terminal operations can be discarded; uncertain operations must sync first")
	})
}

// EditablePayload removes server-only fields from the edit form.
func EditablePayload(kind string, raw json.RawMessage) json.RawMessage {
	var values map[string]json.RawMessage
	_ = json.Unmarshal(raw, &values)
	filtered := map[string]json.RawMessage{}
	for key := range catalogFields[kind] {
		if kind == "ingredient" && key == "base_unit" {
			continue
		}
		if v, ok := values[key]; ok && string(v) != "null" {
			filtered[key] = v
		}
	}
	b, _ := json.MarshalIndent(filtered, "", "  ")
	return b
}

func (op Operation) EntityTypeLabel() string {
	switch op.EntityType {
	case "category":
		return "دسته‌بندی منو"
	case "product":
		return "محصول منو"
	case "ingredient":
		return "کالای انبار"
	case "banner":
		return "بنر دیجیتال"
	case "order":
		return "سفارش صندوق"
	default:
		return op.EntityType
	}
}

func (op Operation) ActionLabel() string {
	switch op.Action {
	case "create":
		return "ایجاد رکورد"
	case "update":
		return "ویرایش رکورد"
	case "delete":
		return "حذف رکورد"
	default:
		return op.Action
	}
}

func (op Operation) StateLabel() string {
	switch op.State {
	case "queued":
		return "در صف (Queued)"
	case "retry":
		return "تلاش مجدد (Retry)"
	case "conflict":
		return "تعارض نسخه (Conflict)"
	case "rejected":
		return "رد شده (Rejected)"
	case "skipped":
		return "رد شده (Skipped)"
	case "applied":
		return "اعمال شده (Applied)"
	case "discarded":
		return "لغو شده (Discarded)"
	default:
		return op.State
	}
}
