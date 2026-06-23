import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

export function buildServerOptions(unobinPath: string): ServerOptions {
  return {
    command: unobinPath,
    args: ['lsp'],
  };
}

export function activate(context: vscode.ExtensionContext): void {
  const config = vscode.workspace.getConfiguration('unobin');
  const unobinPath = config.get<string>('path', 'unobin');
  const serverOptions = buildServerOptions(unobinPath);
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: 'file', language: 'unobin' }],
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher('**/*.ub'),
    },
  };

  client = new LanguageClient('unobin', 'Unobin', serverOptions, clientOptions);
  context.subscriptions.push(client);
  void client.start();
}

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}
