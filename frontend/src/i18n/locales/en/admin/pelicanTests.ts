export default {
  pelicanTests: {
    title: 'Pelican IQ Test',
    description: 'Ask the model to draw a pelican riding a bicycle in HTML/SVG and judge the account by the completeness of the work. This core version runs manual account-level tests and keeps the works in history.',
    form: {
      title: 'Run a test',
      account: 'Account',
      accountPlaceholder: 'Select an account',
      model: 'Model',
      modelPlaceholder: 'Leave empty to use the account default test model',
      prompt: 'Prompt',
      promptPlaceholder: 'Describe the picture to generate',
      resetPrompt: 'Reset prompt',
      run: 'Run test',
      running: 'Generating…'
    },
    history: {
      title: 'Test history',
      empty: 'No tests yet. Pick an account and run a test; the work will appear here.',
      id: 'ID',
      account: 'Account',
      model: 'Model',
      status: 'Result',
      latency: 'Duration',
      createdAt: 'Time',
      actions: 'Actions',
      view: 'View work'
    },
    status: {
      success: 'Success',
      failed: 'Failed'
    },
    messages: {
      done: 'Test finished and the work was saved',
      failed: 'Test failed: {error}',
      loadFailed: 'Failed to load history: {error}',
      loadAccountsFailed: 'Failed to load accounts: {error}',
      selectAccount: 'Select an account first'
    },
    preview: {
      title: 'Pelican work',
      loading: 'Loading work…',
      loadFailed: 'Failed to load the work',
      empty: 'This run produced no renderable work',
      sandboxNote: 'The work runs in an isolated sandbox without network access.',
      close: 'Close'
    }
  }
}
