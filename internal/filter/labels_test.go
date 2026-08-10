package filter

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	pkgfilter "github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

// Test helper - simple mapper that quotes field names
var testMapper pkgfilter.MapFunc = func(name string) (string, pkgfilter.FieldType, error) {
	return fmt.Sprintf(`"%s"`, name), pkgfilter.AnyField, nil
}

var _ = Describe("Labels Filter", func() {
	Context("SQL Generation - contains operator", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== CONTAINS OPERATOR =====
			{input: "labels contains 'production'", output: `list_contains(CAST("labels" AS VARCHAR[]), 'production')`},
			{input: "labels contains 'test'", output: `list_contains(CAST("labels" AS VARCHAR[]), 'test')`},
			{input: "labels contains 'staging'", output: `list_contains(CAST("labels" AS VARCHAR[]), 'staging')`},

			// ===== NOT CONTAINS OPERATOR =====
			{input: "labels not contains 'production'", output: `("labels" IS NULL OR NOT list_contains(CAST("labels" AS VARCHAR[]), 'production'))`},
			{input: "labels not contains 'test'", output: `("labels" IS NULL OR NOT list_contains(CAST("labels" AS VARCHAR[]), 'test'))`},

			// ===== COMBINED WITH AND =====
			{input: "name = 'vm1' and labels contains 'production'", output: `(("name" = 'vm1') AND list_contains(CAST("labels" AS VARCHAR[]), 'production'))`},
			{input: "labels contains 'critical' and cluster = 'prod'", output: `(list_contains(CAST("labels" AS VARCHAR[]), 'critical') AND ("cluster" = 'prod'))`},

			// ===== COMBINED WITH OR =====
			{input: "labels contains 'production' or labels contains 'staging'", output: `(list_contains(CAST("labels" AS VARCHAR[]), 'production') OR list_contains(CAST("labels" AS VARCHAR[]), 'staging'))`},

			// ===== MIXED CONTAINS AND NOT CONTAINS =====
			{input: "labels contains 'production' and labels not contains 'test'", output: `(list_contains(CAST("labels" AS VARCHAR[]), 'production') AND ("labels" IS NULL OR NOT list_contains(CAST("labels" AS VARCHAR[]), 'test')))`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				sqlizer, err := pkgfilter.Parse([]byte(test.input), testMapper)
				Expect(err).ToNot(HaveOccurred())
				sql, err := sqlToString(sqlizer)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("SQL Generation with DefaultMapper", func() {
		It("should map labels to v.\"labels\" column with CAST", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("labels contains 'production'"))
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`list_contains(CAST(v."labels" AS VARCHAR[]), ?)`))
			Expect(args).To(Equal([]interface{}{"production"}))
		})

		It("should map negated contains to NOT list_contains with CAST", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("labels not contains 'test'"))
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`(v."labels" IS NULL OR NOT list_contains(CAST(v."labels" AS VARCHAR[]), ?))`))
			Expect(args).To(Equal([]interface{}{"test"}))
		})

		It("should support complex expressions with labels", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("name = 'vm1' and labels contains 'production'"))
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(ContainSubstring(`v."VM" = ?`))
			Expect(sql).To(ContainSubstring(`list_contains(CAST(v."labels" AS VARCHAR[]), ?)`))
			Expect(args).To(Equal([]interface{}{"vm1", "production"}))
		})
	})

	Context("Validation", func() {
		It("should reject contains on non-array field", func() {
			_, err := ParseWithDefaultMap([]byte("name contains 'test'"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("contains"))
			Expect(err.Error()).To(ContainSubstring("requires an array field"))
		})

		It("should reject contains with non-string value", func() {
			input := "labels contains 123"
			_, err := pkgfilter.Parse([]byte(input), testMapper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected stringLit"))
		})
	})

	Context("Parser errors", func() {
		It("should reject 'not' without 'in' or 'contains'", func() {
			input := "labels not"
			_, err := pkgfilter.Parse([]byte(input), testMapper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected 'in' or 'contains' after 'not'"))
		})

		It("should reject contains without string value", func() {
			input := "labels contains"
			_, err := pkgfilter.Parse([]byte(input), testMapper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected stringLit"))
		})
	})
})
