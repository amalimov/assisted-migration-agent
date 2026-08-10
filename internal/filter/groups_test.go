package filter

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Groups Filter", func() {
	Context("SQL Generation - contains operator", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== CONTAINS OPERATOR =====
			{input: "groups contains 'production-vms'", output: `list_contains(CAST(g.groups AS VARCHAR[]), 'production-vms')`},
			{input: "groups contains 'test'", output: `list_contains(CAST(g.groups AS VARCHAR[]), 'test')`},
			{input: "groups contains 'staging'", output: `list_contains(CAST(g.groups AS VARCHAR[]), 'staging')`},

			// ===== NOT CONTAINS OPERATOR =====
			{input: "groups not contains 'production-vms'", output: `(g.groups IS NULL OR NOT list_contains(CAST(g.groups AS VARCHAR[]), 'production-vms'))`},
			{input: "groups not contains 'test'", output: `(g.groups IS NULL OR NOT list_contains(CAST(g.groups AS VARCHAR[]), 'test'))`},

			// ===== COMBINED WITH AND =====
			{input: "name = 'vm1' and groups contains 'production-vms'", output: `((v."VM" = 'vm1') AND list_contains(CAST(g.groups AS VARCHAR[]), 'production-vms'))`},
			{input: "groups contains 'critical' and cluster = 'prod'", output: `(list_contains(CAST(g.groups AS VARCHAR[]), 'critical') AND (v."Cluster" = 'prod'))`},

			// ===== COMBINED WITH OR =====
			{input: "groups contains 'production-vms' or groups contains 'staging'", output: `(list_contains(CAST(g.groups AS VARCHAR[]), 'production-vms') OR list_contains(CAST(g.groups AS VARCHAR[]), 'staging'))`},

			// ===== MIXED CONTAINS AND NOT CONTAINS =====
			{input: "groups contains 'production-vms' and groups not contains 'test'", output: `(list_contains(CAST(g.groups AS VARCHAR[]), 'production-vms') AND (g.groups IS NULL OR NOT list_contains(CAST(g.groups AS VARCHAR[]), 'test')))`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				sqlizer, err := ParseWithDefaultMap([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := sqlToString(sqlizer)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("SQL Generation with DefaultMapper", func() {
		It("should map groups to g.groups column with CAST", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("groups contains 'production-vms'"))
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`list_contains(CAST(g.groups AS VARCHAR[]), ?)`))
			Expect(args).To(Equal([]interface{}{"production-vms"}))
		})

		It("should handle groups not contains with DefaultMapper", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("groups not contains 'test'"))
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`(g.groups IS NULL OR NOT list_contains(CAST(g.groups AS VARCHAR[]), ?))`))
			Expect(args).To(Equal([]interface{}{"test"}))
		})
	})

	Context("Combined with labels", func() {
		It("should support groups and labels filters together", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("groups contains 'critical' and labels contains 'production'"))
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(ContainSubstring(`list_contains(CAST(g.groups AS VARCHAR[]), ?)`))
			Expect(sql).To(ContainSubstring(`list_contains(CAST(v."labels" AS VARCHAR[]), ?)`))
			Expect(args).To(Equal([]interface{}{"critical", "production"}))
		})
	})
})
