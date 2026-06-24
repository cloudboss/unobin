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

function buildClientOptions(): LanguageClientOptions {
  return {
    documentSelector: [{ scheme: 'file', language: 'unobin' }],
    synchronize: {
      fileEvents: [
        vscode.workspace.createFileSystemWatcher('**/*.ub'),
        vscode.workspace.createFileSystemWatcher('**/*.go'),
        vscode.workspace.createFileSystemWatcher('**/go.mod'),
        vscode.workspace.createFileSystemWatcher('**/project.ub'),
        vscode.workspace.createFileSystemWatcher('**/project-lock.ub'),
      ],
    },
  };
}

function configuredUnobinPath(): string {
  return vscode.workspace.getConfiguration('unobin').get<string>('path', 'unobin');
}

function startLanguageServer(context: vscode.ExtensionContext): void {
  const serverOptions = buildServerOptions(configuredUnobinPath());
  const clientOptions = buildClientOptions();
  client = new LanguageClient('unobin', 'Unobin', serverOptions, clientOptions);
  context.subscriptions.push(client);
  void client.start();
}

export async function restartLanguageServer(
  context: vscode.ExtensionContext,
): Promise<void> {
  const previous = client;
  client = undefined;
  if (previous !== undefined) {
    await previous.stop();
  }
  startLanguageServer(context);
}

export function activate(context: vscode.ExtensionContext): void {
  context.subscriptions.push(
    vscode.commands.registerCommand('unobin.restartLanguageServer', () => {
      void restartLanguageServer(context);
    }),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration('unobin.path')) {
        void restartLanguageServer(context);
      }
    }),
  );
  startLanguageServer(context);
}

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}
