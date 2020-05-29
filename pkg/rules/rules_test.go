package rules

import (
	"errors"
	"testing"

	"github.com/prometheus/prometheus/pkg/rulefmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateBy(t *testing.T) {
	tt := []struct {
		name            string
		rn              RuleNamespace
		expectedExpr    []string
		count, modified int
		expect          error
	}{
		{
			name:  "with no rules",
			rn:    RuleNamespace{},
			count: 0, modified: 0, expect: nil,
		},
		{
			name: "no modifcation",
			rn: RuleNamespace{
				Groups: []rulefmt.RuleGroup{rulefmt.RuleGroup{Name: "WithoutAggregation", Rules: []rulefmt.Rule{
					{Alert: "WithoutAggregation", Expr: "up != 1"},
				}}},
			},
			expectedExpr: []string{"up != 1"},
			count:        1, modified: 0, expect: nil,
		},
		{
			name: "no change in the query but lints with 'without' in the aggregation",
			rn: RuleNamespace{
				Groups: []rulefmt.RuleGroup{rulefmt.RuleGroup{Name: "SkipWithout", Rules: []rulefmt.Rule{
					{Alert: "SkipWithout", Expr: `
						min without(alertmanager) (
							rate(prometheus_notifications_errors_total{job="default/prometheus"}[5m])
						/
							rate(prometheus_notifications_sent_total{job="default/prometheus"}[5m])
						)
						* 100
						> 3
					`},
				}}},
			},
			expectedExpr: []string{`min without(alertmanager) (rate(prometheus_notifications_errors_total{job="default/prometheus"}[5m]) / rate(prometheus_notifications_sent_total{job="default/prometheus"}[5m])) * 100 > 3`},
			count:        1, modified: 1, expect: nil,
		},
		{
			name: "with an aggregation modification",
			rn: RuleNamespace{
				Groups: []rulefmt.RuleGroup{rulefmt.RuleGroup{Name: "WithAggregation", Rules: []rulefmt.Rule{
					{Alert: "WithAggregation", Expr: `
						sum(rate(cortex_prometheus_rule_evaluation_failures_total[1m])) by (namespace, job)
						/
						sum(rate(cortex_prometheus_rule_evaluations_total[1m])) by (namespace, job)
						> 0.01
					`},
				}}},
			},
			expectedExpr: []string{"sum by(namespace, job, cluster) (rate(cortex_prometheus_rule_evaluation_failures_total[1m])) / sum by(namespace, job, cluster) (rate(cortex_prometheus_rule_evaluations_total[1m])) > 0.01"},
			count:        1, modified: 1, expect: nil,
		},
		{
			name: "with 'count' as the aggregation",
			rn: RuleNamespace{
				Groups: []rulefmt.RuleGroup{rulefmt.RuleGroup{Name: "CountAggregation", Rules: []rulefmt.Rule{
					{Alert: "CountAggregation", Expr: `
						count(count by (gitVersion) (label_replace(kubernetes_build_info{job!~"kube-dns|coredns"},"gitVersion","$1","gitVersion","(v[0-9]*.[0-9]*.[0-9]*).*"))) > 1	
					`},
				}}},
			},
			expectedExpr: []string{`count by(cluster) (count by(gitVersion, cluster) (label_replace(kubernetes_build_info{job!~"kube-dns|coredns"}, "gitVersion", "$1", "gitVersion", "(v[0-9]*.[0-9]*.[0-9]*).*"))) > 1`},
			count:        1, modified: 1, expect: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			c, m, err := tc.rn.AggregateBy("cluster")

			require.Equal(t, tc.expect, err)
			assert.Equal(t, tc.count, c)
			assert.Equal(t, tc.modified, m)

			// Only verify the PromQL expression if it has been modified
			for _, g := range tc.rn.Groups {
				for i, r := range g.Rules {
					require.Equal(t, tc.expectedExpr[i], r.Expr)
				}
			}
		})
	}
}

func TestLintPromQLExpressions(t *testing.T) {
	tt := []struct {
		name            string
		expr            string
		expected        string
		err             error
		count, modified int
	}{
		{
			name:     "it lints simple expressions",
			expr:     "up                                   != 1",
			expected: "up != 1",
			count:    1, modified: 1,
			err: nil,
		},
		{
			name:     "it lints aggregations expressions",
			expr:     "avg (rate(prometheus_notifications_queue_capacity[5m])) by (cluster, job)",
			expected: "avg by(cluster, job) (rate(prometheus_notifications_queue_capacity[5m]))",
			count:    1, modified: 1,
			err: nil,
		},
		{
			name:     "with no opinion",
			expr:     "build_tag_info > 1",
			expected: "build_tag_info > 1",
			count:    1, modified: 0,
			err: nil,
		},
		{
			name:     "with an invalid expression",
			expr:     "it fails",
			expected: "it fails",
			count:    0, modified: 0,
			err: errors.New(`parse error at char 4: could not parse remaining input "fails"...`),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			r := RuleNamespace{Groups: []rulefmt.RuleGroup{{Rules: []rulefmt.Rule{
				{Alert: "AName", Expr: tc.expr},
			}}}}

			c, m, err := r.LintPromQLExpressions()
			rexpr := r.Groups[0].Rules[0].Expr

			require.Equal(t, tc.count, c)
			require.Equal(t, tc.modified, m)
			require.Equal(t, tc.expected, rexpr)

			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.err.Error())
			}
		})
	}
}
