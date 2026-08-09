-- Reverse 0020.
DROP TRIGGER IF EXISTS journal_transactions_period_guard ON journal_transactions;
DROP TRIGGER IF EXISTS journal_entries_guard_trigger ON journal_entries;
DROP TRIGGER IF EXISTS journal_transactions_guard_trigger ON journal_transactions;
DROP TRIGGER IF EXISTS journal_transactions_balance_check ON journal_transactions;
DROP TRIGGER IF EXISTS journal_entries_balance_check ON journal_entries;

DROP FUNCTION IF EXISTS assert_period_open();
DROP FUNCTION IF EXISTS journal_entries_guard();
DROP FUNCTION IF EXISTS journal_transactions_guard();
DROP FUNCTION IF EXISTS assert_journal_balanced_on_post();
DROP FUNCTION IF EXISTS assert_journal_balanced();

DROP TABLE IF EXISTS journal_entries;
DROP TABLE IF EXISTS journal_transactions;
DROP TABLE IF EXISTS ledger_accounts;
DROP TABLE IF EXISTS accounting_periods;

-- btree_gist is left installed: dropping an extension another migration may
-- later rely on is riskier than leaving it, and it holds no data.
