# Where these fixtures come from

Copied verbatim from the M2 review package (EdgeHybrid_Code_Review) at
`evidence/synthetic_summary_inputs/`. The review executed the M2 verifier against
these inputs and reproduced exit code 0 for the empty, garbage and truncated
cases, which was wrong: unusable evidence was being reported as success.

These are ARTIFICIAL inputs for testing the test-result SUMMARIZER. They are NOT
protocol tests and NOT cryptographic test data. `failure_control.jsonl` is the
only one that legitimately reports a failure.
