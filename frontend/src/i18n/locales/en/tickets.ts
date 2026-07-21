export default {
  tickets: {
    title: 'Support Tickets',
    description: 'Report issues and track their progress',
    newTicket: 'New ticket',
    searchPlaceholder: 'Search ticket number or subject',
    refresh: 'Refresh',
    buckets: { all: 'All', active: 'Active', waiting_user: 'Waiting for me', ended: 'Ended' },
    status: { open: 'Open', in_progress: 'In progress', waiting_user: 'Waiting for you', resolved: 'Resolved', closed: 'Closed' },
    category: { api_issue: 'API issue', subscription: 'Subscription', payment: 'Payment', account: 'Account', feature_request: 'Feature request', other: 'Other' },
    impact: { blocked: 'Blocked', degraded: 'Degraded', general: 'General question' },
    priority: { urgent: 'Urgent', high: 'High', normal: 'Normal', low: 'Low' },
    columns: { subject: 'Subject', status: 'Status', category: 'Category', updated: 'Last updated' },
    empty: 'No tickets',
    emptyDescription: 'Create a ticket when you need help from an administrator.',
    unread: 'New reply',
    create: {
      title: 'New ticket', category: 'Category', impact: 'Impact', subject: 'Subject', description: 'Description',
      subjectPlaceholder: 'Briefly describe the issue', bodyPlaceholder: 'Include steps to reproduce, expected behavior, and actual behavior',
      relatedResource: 'Related resource', usageLog: 'Usage log ID', apiKey: 'API key ID', order: 'Order ID', subscription: 'Subscription ID',
      optionalIdPlaceholder: 'Optional; enter only an ID that belongs to you', submit: 'Submit ticket', submitting: 'Submitting', cancel: 'Cancel',
      leaveWarning: 'Your ticket has not been submitted. Leave this page?', failed: 'Failed to create ticket'
    },
    detail: {
      title: 'Ticket details', back: 'Back to tickets', context: 'Related information', conversation: 'Conversation', noConversation: 'No conversation yet',
      replyPlaceholder: 'Add more information', send: 'Send reply', close: 'Close ticket', confirmResolved: 'Confirm resolved', reopen: 'Still not resolved',
      reopenPlaceholder: 'Describe what is still not resolved', closedHint: 'This ticket is closed and no longer accepts replies.',
      refreshConflict: 'The ticket changed. Details were refreshed and your draft was kept.', actionFailed: 'Action failed', downloadFailed: 'Attachment download failed'
    },
    attachments: {
      add: 'Add attachment', uploading: 'Uploading', remove: 'Remove', disabled: 'Attachment uploads are disabled',
      limits: 'PNG, JPEG, WebP, TXT, and JSON are supported', tooMany: 'Attachment limit exceeded', uploadFailed: 'Attachment upload failed'
    },
    events: {
      ticket_created: 'Ticket created', ticket_claimed: 'An administrator claimed the ticket', ticket_assigned: 'Assignee updated', priority_changed: 'Priority updated',
      status_changed: 'Ticket status updated', ticket_resolved: 'Ticket marked as resolved', ticket_reopened: 'Ticket reopened',
      ticket_closed: 'Ticket closed', ticket_auto_closed: 'Ticket automatically closed after the reopen window ended'
    },
    time: { justNow: 'Just now' }
  }
}
