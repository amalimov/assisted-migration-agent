package filter

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	pkgfilter "github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

var _ = Describe("Migration Excluded Filter", func() {
	Context("migration_excluded field mapping", func() {
		It("should map migration_excluded to correct column with BooleanField type", func() {
			column, fieldType, err := DefaultMapper("migration_excluded")

			Expect(err).NotTo(HaveOccurred())
			Expect(column).To(Equal(`v."migration_excluded"`))
			Expect(fieldType).To(Equal(pkgfilter.BooleanField))
		})
	})

	Context("migration_excluded filter expressions", func() {
		type testCase struct {
			input       string
			expectedSQL string
			description string
		}

		tests := []testCase{
			{
				input:       "migration_excluded = true",
				expectedSQL: `(v."migration_excluded" = TRUE)`,
				description: "filter for excluded VMs",
			},
			{
				input:       "migration_excluded = false",
				expectedSQL: `(v."migration_excluded" = FALSE)`,
				description: "filter for included VMs",
			},
			{
				input:       "migration_excluded != true",
				expectedSQL: `(v."migration_excluded" != TRUE)`,
				description: "filter for not excluded VMs using !=",
			},
			{
				input:       "migration_excluded = true and cluster = 'prod'",
				expectedSQL: `((v."migration_excluded" = TRUE) AND (v."Cluster" = 'prod'))`,
				description: "combine migration_excluded with cluster filter",
			},
			{
				input:       "migration_excluded = false and migratable = true",
				expectedSQL: `((v."migration_excluded" = FALSE) AND ((COALESCE(crit.critical_count, 0) = 0) = TRUE))`,
				description: "combine migration_excluded with migratable filter",
			},
			{
				input:       "cluster = 'staging' and migration_excluded = true",
				expectedSQL: `((v."Cluster" = 'staging') AND (v."migration_excluded" = TRUE))`,
				description: "cluster filter before migration_excluded",
			},
			{
				input:       "(migration_excluded = true or cluster = 'test') and powerstate = 'poweredOn'",
				expectedSQL: `(((v."migration_excluded" = TRUE) OR (v."Cluster" = 'test')) AND (v."Powerstate" = 'poweredOn'))`,
				description: "complex expression with OR and AND",
			},
		}

		for _, test := range tests {
			test := test
			It("should generate correct SQL for: "+test.description, func() {
				sqlizer, err := ParseWithDefaultMap([]byte(test.input))
				Expect(err).NotTo(HaveOccurred())

				sql, err := sqlToString(sqlizer)
				Expect(err).NotTo(HaveOccurred())
				Expect(sql).To(Equal(test.expectedSQL))
			})
		}
	})

	Context("ParseWithDefaultMap integration", func() {
		It("should parse migration_excluded = true", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("migration_excluded = true"))

			Expect(err).NotTo(HaveOccurred())
			Expect(sqlizer).NotTo(BeNil())

			sql, args, err := sqlizer.ToSql()
			Expect(err).NotTo(HaveOccurred())
			Expect(sql).To(ContainSubstring(`v."migration_excluded"`))
			Expect(sql).To(ContainSubstring("= TRUE"))
			Expect(args).To(HaveLen(0)) // Boolean values are embedded directly, not placeholders
		})

		It("should parse migration_excluded = false", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("migration_excluded = false"))

			Expect(err).NotTo(HaveOccurred())
			Expect(sqlizer).NotTo(BeNil())

			sql, args, err := sqlizer.ToSql()
			Expect(err).NotTo(HaveOccurred())
			Expect(sql).To(ContainSubstring(`v."migration_excluded"`))
			Expect(sql).To(ContainSubstring("= FALSE"))
			Expect(args).To(HaveLen(0)) // Boolean values are embedded directly, not placeholders
		})

		It("should parse complex expression with migration_excluded", func() {
			expression := `migration_excluded = false and cluster = "production" and memory >= 8192`
			sqlizer, err := ParseWithDefaultMap([]byte(expression))

			Expect(err).NotTo(HaveOccurred())
			Expect(sqlizer).NotTo(BeNil())

			sql, _, err := sqlizer.ToSql()
			Expect(err).NotTo(HaveOccurred())
			Expect(sql).To(ContainSubstring(`v."migration_excluded"`))
			Expect(sql).To(ContainSubstring(`v."Cluster"`))
			Expect(sql).To(ContainSubstring(`v."Memory"`))
		})
	})

	Context("Error handling", func() {
		It("should reject non-boolean values for boolean fields", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("migration_excluded = 'invalid'"))

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boolean"))
			Expect(sqlizer).To(BeNil())
		})
	})
})
