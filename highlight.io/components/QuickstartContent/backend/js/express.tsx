import { siteUrl } from '../../../../utils/urls'
import { QuickStartContent } from '../../QuickstartContent'
import { frontendInstallSnippet } from '../shared-snippets'
import {
	addIntegrationContent,
	initializeNodeSDK,
	jsGetSnippet,
	setupLogging,
	verifyError,
} from './shared-snippets'

export const JSExpressContent: QuickStartContent = {
	title: 'Express.js',
	subtitle: 'Learn how to set up highlight.io in Express.js.',
	logoUrl: siteUrl('/images/quickstart/express.svg'),
	entries: [
		frontendInstallSnippet,
		jsGetSnippet(['node']),
		initializeNodeSDK('node'),
		{
			title: `Add the Express.js Highlight integration.`,
			content: addIntegrationContent('Node Highlight SDK', 'nodejs'),
			code: [
				{
					text: `import { H, Handlers } from '@highlight-run/node'
// or like this with commonjs
// const { H, Highlight } = require('@highlight-run/node')

const app = express()

const highlightConfig = {
	projectID: '<YOUR_PROJECT_ID>',
	serviceName: 'my-express-app',
	serviceVersion: 'git-sha'
}
H.init(highlightConfig)

// This should be before any controllers (route definitions)
app.use(Handlers.middleware(highlightConfig))

app.get('/', (req, res) => {
  res.send(\`Hello World! ${Math.random()}\`)
})

// This should be before any other error middleware and after all controllers (route definitions)
app.use(Handlers.errorHandler(highlightConfig))

app.listen(8080, () => {
  console.log(\`Example app listening on port 8080\`)
})`,
					language: `js`,
				},
			],
		},
		{
			title: `Try/catch an error manually (without middleware).`,
			content:
				'If you are using express.js async handlers, you will need your own try/catch block that directly calls the highlight SDK to report an error. ' +
				'This is because express.js async handlers do not invoke error middleware.',
			code: [
				{
					text: `app.get('/sync', (req: Request, res: Response) => {
	// do something dangerous...
	throw new Error('oh no! this is a synchronous error');
});

app.get('/async', async (req: Request, res: Response) => {
  try {
    // do something dangerous...
    throw new Error('oh no!');
  } catch (error) {
    const { secureSessionId, requestId } = H.parseHeaders(req.headers);
    H.consumeError(
      error as Error,
      secureSessionId,
      requestId
    );
  } finally {
    res.status(200).json({hello: 'world'});
  }
});`,
					language: `js`,
				},
			],
		},
		verifyError(
			'express.js',
			`app.get('/', (req, res) => {
  throw new Error('sample error!')
  res.send(\`Hello World! ${Math.random()}\`)
})`,
		),
		setupLogging('express'),
	],
}
