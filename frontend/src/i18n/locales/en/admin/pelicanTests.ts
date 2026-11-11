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
    plans: {
      title: 'Scheduled plans',
      empty: 'No scheduled plans yet. Create one to run the Pelican test automatically on a cron schedule.',
      create: 'New plan',
      edit: 'Edit',
      delete: 'Delete',
      runNow: 'Run now',
      running: 'Running…',
      enabled: 'Enabled',
      paused: 'Paused',
      account: 'Account',
      model: 'Model',
      cron: 'Cron',
      nextRun: 'Next run',
      lastRun: 'Last run',
      lastStatus: 'Last result',
      never: 'Not run yet',
      actions: 'Actions',
      deleteTitle: 'Delete scheduled plan',
      deleteConfirm: 'The plan will stop running automatically. Saved works and history are kept. Delete it?',
      messages: {
        created: 'Scheduled plan created',
        updated: 'Scheduled plan saved',
        deleted: 'Scheduled plan deleted',
        saveFailed: 'Failed to save plan: {error}',
        deleteFailed: 'Failed to delete plan: {error}',
        runFailed: 'Run now failed: {error}',
        runDone: 'Plan executed and the work was saved'
      },
      editor: {
        createTitle: 'New scheduled plan',
        editTitle: 'Edit scheduled plan',
        account: 'Account',
        model: 'Model',
        prompt: 'Prompt',
        cron: 'Cron expression',
        cronHint: 'Standard 5-field cron in the server timezone.',
        enabled: 'Enabled',
        save: 'Save',
        cancel: 'Cancel',
        required: 'Select an account and enter a cron expression'
      }
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
