export default {
  tickets: {
    title: 'Ticket Management', description: 'Handle user reports and internal collaboration', settings: 'Ticket settings',
    buckets: { action_required: 'Action required', in_progress: 'In progress', waiting_user: 'Waiting for user', ended: 'Ended', all: 'All' },
    filters: { search: 'Search tickets, users, requests, or orders', category: 'All categories', priority: 'All priorities', assignment: 'Assignee', allAssignees: 'All assignees', unassigned: 'Unassigned', createdFrom: 'Created from', createdTo: 'Created to' },
    columns: { ticket: 'Ticket', requester: 'Requester', priority: 'Priority', assignee: 'Assignee', status: 'Status', waiting: 'Waiting' },
    detail: {
      title: 'Manage ticket', back: 'Back to ticket queue', claim: 'Claim', unassign: 'Unassign', resolve: 'Mark resolved', reopen: 'Reopen', close: 'Close ticket',
      closeReason: 'Close reason', closeReasonPlaceholder: 'Explain why this ticket is being closed; the user can see it', assignment: 'Assignee', priority: 'Priority',
      replyUser: 'Reply to user', internalNote: 'Internal note', internalOnly: 'Administrators only', bodyPlaceholder: 'Write a response',
      nextAction: 'After sending', waitUser: 'Wait for user', keepProcessing: 'Keep processing', replyAndResolve: 'Reply and resolve', send: 'Send',
      conflict: 'Another administrator updated this ticket. Details were refreshed and your draft was kept.', operationFailed: 'Operation failed'
    },
    storage: {
      title: 'Ticket attachment storage', description: 'Changing modes affects only new uploads. Existing attachments are not moved or deleted.',
      modes: { disabled: 'Disable uploads', local: 'Local storage', s3: 'S3 storage' },
      localPath: 'Local directory', localWritable: 'Directory is writable', localUnavailable: 'Directory is unavailable', sharedVolume: 'Shared persistent volume',
      multiInstanceWarning: 'For multiple instances, every instance must mount the same persistent volume before local storage is enabled.',
      endpoint: 'Endpoint', region: 'Region', bucket: 'Bucket', accessKey: 'Access Key', secretKey: 'Secret Key', prefix: 'Object prefix', pathStyle: 'Force path style',
      secretConfigured: 'Configured; leave blank to keep the current secret', secretPlaceholder: 'Leave blank to keep the current secret',
      usage: 'Existing attachment usage', files: '{count} files', test: 'Test connection', testing: 'Testing', save: 'Save and enable', saving: 'Saving',
      testSuccess: 'Storage probe succeeded', saveSuccess: 'Ticket attachment settings saved', failed: 'Storage configuration operation failed', destinationInUse: 'Existing S3 attachments use this destination. Endpoint, region, bucket, prefix, and path style cannot be changed.'
    }
  }
}
