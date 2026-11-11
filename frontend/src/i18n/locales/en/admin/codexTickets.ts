export default {
  codexTickets: {
    title: 'Codex Tickets',
    description: 'Mint x-codex-turn-state tickets for Codex OAuth accounts in the background. When an account has no ticket, the site-wide shared pool can lend one.',
    settings: {
      title: 'Ticket settings',
      enabled: 'Enable automatic minting',
      enabledHint: 'The background harvester mints tickets for schedulable accounts and injects them into requests. No upstream probe happens while disabled.',
      shared: 'Site-wide shared tickets',
      sharedHint: 'When an account has no valid ticket of its own, it may borrow one from other accounts. Its own ticket always wins.',
      failClosed: 'Reject requests without a ticket',
      failClosedHint: 'When enabled, gated-model requests with neither an own ticket nor a borrowable ticket fail instead of passing through. Off by default.',
      models: 'Ticket models',
      modelsHint: 'Comma separated. Only these models are minted and injected. Leave empty to disable ticket minting and injection.',
      interval: 'Harvest interval (seconds)',
      intervalHint: 'Minimum delay between background rounds, 30–3600 seconds.',
      ttl: 'Ticket TTL (seconds)',
      ttlHint: 'Maximum stored lifetime, 60–86400 seconds. The ticket’s own issue time may expire it sooner.',
      probeTimeout: 'Probe timeout (seconds)',
      probeTimeoutHint: 'Timeout for one upstream probe, 5–120 seconds.'
    },
    actions: {
      save: 'Save settings',
      saving: 'Saving…',
      refresh: 'Refresh',
      harvestAll: 'Harvest now',
      harvest: 'Harvest',
      harvesting: 'Harvesting…',
      iqTest: 'IQ test',
      iqTesting: 'Testing…'
    },
    status: {
      ready: 'Ready',
      own: 'Own ticket',
      shared: 'Shared ticket',
      missing: 'No ticket',
      expiresIn: '{time} left',
      source: 'from #{id}',
      iqHealthy: 'Full power',
      iqDegraded: 'Degraded',
      iqInconclusive: 'Inconclusive'
    },
    pool: {
      title: 'Site-wide shared pool',
      empty: 'The shared pool is empty. Tickets appear here after automatic or manual harvesting.',
      model: 'Model',
      source: 'Source account',
      remaining: 'Remaining',
      length: 'Length',
      captured: 'Captured'
    },
    accounts: {
      title: 'Account ticket status',
      empty: 'No Codex OAuth account is eligible for ticket harvesting.',
      account: 'Account',
      tickets: 'Tickets',
      actions: 'Actions'
    },
    messages: {
      saved: 'Ticket settings saved',
      harvested: 'Harvest finished: {ok} ok, {failed} failed, {skipped} skipped',
      failed: 'Operation failed: {error}',
      loadFailed: 'Failed to load ticket status: {error}',
      noModels: 'Configure at least one ticket model first',
      iqDone: 'IQ test finished: {verdict} ({ms} ms)'
    },
    iq: {
      title: 'IQ test',
      weight: 'IQ',
      latency: '{ms} ms',
      ticket: 'Ticket {length}',
      newTicket: 'Continuation returned a new ticket',
      shortTicket: 'Short-ticket form, treat as indicative only',
      testedAt: 'Tested at {time}',
      reason: 'Basis'
    },
    manage: {
      title: 'Ticket Management',
      description: 'Inspect, refresh and delete Codex tickets across the site. A deleted ticket is minted again on the next request or harvest round.',
      filters: {
        accountId: 'Account ID',
        accountIdPlaceholder: 'All accounts',
        model: 'Model',
        modelPlaceholder: 'All models',
        status: 'Status',
        allStatus: 'All statuses',
        valid: 'Valid',
        expired: 'Expired',
        search: 'Search',
        reset: 'Reset'
      },
      table: {
        id: 'ID',
        model: 'Model',
        account: 'Account',
        status: 'Status',
        length: 'Length',
        acquisition: 'Source',
        captured: 'Captured',
        expires: 'Expires',
        remaining: 'Remaining',
        actions: 'Actions'
      },
      actions: {
        refresh: 'Refresh',
        refreshing: 'Refreshing…',
        delete: 'Delete',
        deleting: 'Deleting…'
      },
      status: {
        valid: 'Valid',
        expired: 'Expired'
      },
      messages: {
        deleted: 'Ticket deleted',
        refreshed: 'Refresh finished: {ok} ok, {failed} failed',
        refreshFailed: 'Refresh failed: {error}',
        deleteFailed: 'Delete failed: {error}',
        loadFailed: 'Failed to load tickets: {error}',
        deleteConfirm: 'Delete this ticket? The account will mint a new one on the next request or harvest round.'
      },
      pagination: {
        prev: 'Previous',
        next: 'Next',
        info: 'Page {page} / {pages} · {total} tickets'
      },
      empty: 'No tickets'
    },
    lastRound: 'Last round: {time}',
    never: 'Not harvested yet'
  }
}
